# frozen_string_literal: true

# Runs a child process with standard output and standard error joined, and kills
# its whole process group when it outlives a deadline, so a hung child fails the
# spec instead of stalling the job.
module CommandRunner
  POLL_INTERVAL_SECONDS = 0.1

  Result = Struct.new(:output, :exit_status, :timed_out, keyword_init: true)
  CaptureResult = Struct.new(:output, :error_output, :exit_status, :timed_out, keyword_init: true)

  module_function

  # Runs a child process with stdin_data on standard input and captures standard
  # output apart from standard error, so a warning on standard error cannot
  # corrupt output the caller parses.
  def capture(argv, stdin_data:, chdir:, timeout_seconds:)
    input_reader, input_writer = IO.pipe
    output_reader, output_writer = IO.pipe
    error_reader, error_writer = IO.pipe
    process_id = Process.spawn(*argv, chdir: chdir, pgroup: true, in: input_reader, out: output_writer, err: error_writer)
    [input_reader, output_writer, error_writer].each(&:close)
    output_thread = Thread.new { output_reader.read }
    error_thread = Thread.new { error_reader.read }
    input_thread = Thread.new { write_input(input_writer, stdin_data) }
    exit_status = wait_until(process_id, monotonic_seconds + timeout_seconds)
    timed_out = exit_status.nil?
    exit_status = kill_group(process_id) if timed_out
    input_thread.join
    CaptureResult.new(output: output_thread.value, error_output: error_thread.value, exit_status: exit_status, timed_out: timed_out)
  ensure
    [input_writer, output_reader, error_reader].each { |stream| stream&.close unless stream&.closed? }
  end

  # A child that exits before reading all of its input closes the pipe, and
  # its exit status then reports the failure.
  def write_input(input_writer, stdin_data)
    input_writer.write(stdin_data)
  rescue Errno::EPIPE
    nil
  ensure
    input_writer.close
  end

  def run(environment, argv, chdir:, timeout_seconds:)
    output_reader, output_writer = IO.pipe
    process_id = Process.spawn(environment, *argv, chdir: chdir, pgroup: true, %i[out err] => output_writer)
    output_writer.close
    output_thread = Thread.new { output_reader.read }
    deadline = monotonic_seconds + timeout_seconds
    exit_status = wait_until(process_id, deadline)
    timed_out = exit_status.nil?
    exit_status = kill_group(process_id) if timed_out
    Result.new(output: output_thread.value, exit_status: exit_status, timed_out: timed_out)
  ensure
    output_reader&.close
  end

  def wait_until(process_id, deadline)
    loop do
      _, exit_status = Process.wait2(process_id, Process::WNOHANG)
      return exit_status if exit_status
      return nil if monotonic_seconds >= deadline

      sleep POLL_INTERVAL_SECONDS
    end
  end

  def kill_group(process_id)
    Process.kill('KILL', -process_id)
    _, exit_status = Process.wait2(process_id)
    exit_status
  end

  def monotonic_seconds
    Process.clock_gettime(Process::CLOCK_MONOTONIC)
  end
end
