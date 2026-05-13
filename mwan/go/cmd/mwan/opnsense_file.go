package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsense"
)

// fileVerb enumerates `mwan opnsense file <verb>` sub-verbs.
type fileVerb string

const (
	fileVerbPush fileVerb = "push"
	fileVerbPull fileVerb = "pull"
)

func fileUsage(out *os.File) {
	fmt.Fprintln(out, "usage: mwan opnsense file <verb> [args...]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Verbs:")
	fmt.Fprintln(out, "  push SOURCE PATH   upload local SOURCE to remote PATH on the OPNsense guest")
	fmt.Fprintln(out, "  pull PATH DEST     download remote PATH to local DEST")
}

func runOPNsenseFile(args []string) int {
	if len(args) < 1 {
		fileUsage(os.Stderr)
		return 2
	}
	verb := fileVerb(args[0])
	rest := args[1:]
	switch verb {
	case fileVerbPush:
		return runFilePush(rest)
	case fileVerbPull:
		return runFilePull(rest)
	default:
		fmt.Fprintf(os.Stderr, "mwan opnsense file: unknown verb %q\n", string(verb))
		fileUsage(os.Stderr)
		return 2
	}
}

// runFilePush uploads SOURCE to PATH on the OPNsense guest. The upload
// chunk size comes from [opnsense.probe].upload_chunk_bytes; the target
// socket and timeout come from the same section.
func runFilePush(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan opnsense file push SOURCE PATH")
		return 2
	}
	source := filepath.Clean(args[0])
	remotePath := args[1]
	cfg, err := loadOpnsenseConfig()
	if err != nil {
		return printAndExit("file push", err)
	}
	chunk, err := requireProbeUploadChunk(cfg)
	if err != nil {
		return printAndExit("file push", err)
	}
	cli, ctx, cancel, err := dialProbe()
	if err != nil {
		return printAndExit("file push", err)
	}
	defer cancel()
	defer func() { _ = cli.Close() }()

	payload, err := os.ReadFile(source)
	if err != nil {
		return printAndExit("file push", fmt.Errorf("read %s: %w", source, err))
	}
	if err := streamFilePush(ctx, cli, payload, remotePath, chunk); err != nil {
		return printAndExit("file push", err)
	}
	return 0
}

func streamFilePush(ctx context.Context, cli *opnsense.Client, payload []byte, remotePath string, chunk int) error {
	size := int64(len(payload))
	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		return wrapErr(ctx, "file push: open upload stream", err)
	}
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:       remotePath,
			Direction:  mwanv1.TransferDirection_TRANSFER_DIRECTION_WRITE,
			FinishStep: mwanv1.FinishStep_FINISH_STEP_REPLACE,
			TotalSize:  size,
		}},
	}); sendErr != nil {
		return wrapErr(ctx, "file push: send header", sendErr)
	}
	ackMsg, recvErr := stream.Recv()
	if recvErr != nil {
		return wrapErr(ctx, "file push: recv header ack", recvErr)
	}
	if ackMsg.GetAck() == nil {
		return wrapErr(ctx, "file push: header ack missing", errors.New("first response was not an ack"))
	}
	hasher := sha256.New()
	if err := streamPushData(ctx, stream, payload, size, hasher, chunk); err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	finalHex := hex.EncodeToString(sum[:])
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Final{Final: &mwanv1.TransferFinal{Sha256Hex: finalHex}},
	}); sendErr != nil {
		return wrapErr(ctx, "file push: send final", sendErr)
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return wrapErr(ctx, "file push: close send", closeErr)
	}
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return wrapErr(ctx, "file push: stream ended", errors.New("no terminal message"))
		}
		if recvErr != nil {
			return wrapErr(ctx, "file push: recv terminal", recvErr)
		}
		if term := msg.GetTerminal(); term != nil {
			fmt.Printf("file push: bytes=%d sha256=%s backup_path=%s\n",
				term.GetTotalBytes(), term.GetSha256Hex(), term.GetBackupPath())
			return nil
		}
	}
}

func streamPushData(ctx context.Context, stream mwanv1.TransferService_UploadClient, payload []byte, size int64, hasher hash.Hash, chunk int) error {
	offset := int64(0)
	for offset < size {
		end := min(offset+int64(chunk), size)
		segment := payload[offset:end]
		if _, hashErr := hasher.Write(segment); hashErr != nil {
			return wrapErr(ctx, "file push: hash", hashErr)
		}
		if sendErr := stream.Send(&mwanv1.UploadRequest{
			Body: &mwanv1.UploadRequest_Data{Data: &mwanv1.TransferDataChunk{
				Offset: offset,
				Data:   segment,
			}},
		}); sendErr != nil {
			return wrapErr(ctx, "file push: send data", sendErr)
		}
		ackMsg, recvErr := stream.Recv()
		if recvErr != nil {
			return wrapErr(ctx, "file push: recv data ack", recvErr)
		}
		dataAck, ok := ackMsg.GetBody().(*mwanv1.UploadResponse_DataAck)
		if !ok {
			return wrapErr(ctx, "file push: expected data ack", fmt.Errorf("got %T at offset %d", ackMsg.GetBody(), end))
		}
		if dataAck.DataAck.GetCommittedOffset() != end {
			return wrapErr(ctx, "file push: data ack offset", fmt.Errorf("got %d want %d", dataAck.DataAck.GetCommittedOffset(), end))
		}
		offset = end
	}
	return nil
}

// runFilePull downloads PATH from the OPNsense guest and writes it to
// DEST locally.
func runFilePull(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan opnsense file pull PATH DEST")
		return 2
	}
	remotePath := args[0]
	dest := filepath.Clean(args[1])
	cli, ctx, cancel, err := dialProbe()
	if err != nil {
		return printAndExit("file pull", err)
	}
	defer cancel()
	defer func() { _ = cli.Close() }()

	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		return printAndExit("file pull", fmt.Errorf("open stream: %w", err))
	}
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:      remotePath,
			Direction: mwanv1.TransferDirection_TRANSFER_DIRECTION_READ,
		}},
	}); sendErr != nil {
		return printAndExit("file pull", fmt.Errorf("send header: %w", sendErr))
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return printAndExit("file pull", fmt.Errorf("close send: %w", closeErr))
	}

	hasher := sha256.New()
	total := int64(0)
	out, err := os.Create(dest)
	if err != nil {
		return printAndExit("file pull", fmt.Errorf("create %s: %w", dest, err))
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil {
			slog.WarnContext(ctx, "file pull: close dest", "err", closeErr, "path", dest)
		}
	}()
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return printAndExit("file pull", fmt.Errorf("recv: %w", recvErr))
		}
		if data := msg.GetData(); data != nil {
			buf := data.GetData()
			if _, hashErr := hasher.Write(buf); hashErr != nil {
				return printAndExit("file pull", fmt.Errorf("hash: %w", hashErr))
			}
			if _, writeErr := out.Write(buf); writeErr != nil {
				return printAndExit("file pull", fmt.Errorf("write %s: %w", dest, writeErr))
			}
			total += int64(len(buf))
		}
		if term := msg.GetTerminal(); term != nil {
			fmt.Printf("file pull: bytes=%d sha256=%s server_sha256=%s dest=%s\n",
				total, hex.EncodeToString(hasher.Sum(nil)), term.GetSha256Hex(), dest)
			return 0
		}
	}
	return 0
}
