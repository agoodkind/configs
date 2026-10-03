# frozen_string_literal: true

require 'tmpdir'
require_relative '../support/tack_ops_identity'

# These examples run the real tack-ops playbook in check mode on one local
# host in tack_qa_all. tack_install_dir is a directory that does not exist:
# a run that passes the request check stops at the chdir of the verify task,
# before the command module runs docker.
#
# The audit row of ops deploy verify records deploy_commit and the agent
# identity, and no command flag. The play command line and the ops deploy
# verify result line record both expected digests, and the error records them
# on a mismatch. The QA reset and production run evidence save both lines.
module TackOpsDigests
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tack-ops.yml')
  INVENTORY = "[tack_qa_all]\ntack-qa-test ansible_connection=local\n[tack_prod_all]\n" \
              "[tack_all:children]\ntack_qa_all\ntack_prod_all\n"
  REQUEST_TASK = 'TASK [Refuse a Tack ops request outside the reviewed commands, guests, or arguments]'
  REQUEST_MESSAGE = 'tack-ops runs one reviewed Tack ops command on exactly one QA guest'
  VERIFY_TASK = 'TASK [Verify that the guest runs the Tack images for tack_commit]'
  SERVER_DIGEST = "sha256:#{'a' * 64}".freeze
  CONSUMER_DIGEST = "sha256:#{'b' * 64}".freeze

  module_function

  # Runs tack-ops as a human operator with the given digests.
  def run(server_digest:, consumer_digest:)
    Dir.mktmpdir('tack-ops-digests') do |directory|
      variables = TackOpsIdentityFlags.identity(service: '', session: '').merge(
        'tack_ops_command' => 'ops search verify', 'tack_ops_args' => [], 'tack_ops_execute' => false,
        'tack_server_digest' => server_digest, 'tack_audit_consumer_digest' => consumer_digest,
        'tack_install_dir' => File.join(directory, 'absent')
      )
      TackOpsIdentityFlags.run_deploy(variables, agent: false, playbook: PLAYBOOK_FILE, inventory_text: INVENTORY)
    end
  end
end

RSpec.describe TackOpsDigests do
  it 'passes the request check with both digests and stops at the verify chdir', :aggregate_failures do
    result = described_class.run(server_digest: TackOpsDigests::SERVER_DIGEST, consumer_digest: TackOpsDigests::CONSUMER_DIGEST)

    expect(result.exit_status.success?).to be(false)
    expect(result.output).not_to include(TackOpsDigests::REQUEST_MESSAGE)
    expect(result.output).to include(TackOpsDigests::VERIFY_TASK)
  end

  it 'refuses a missing digest before the verify task', :aggregate_failures do
    [{ server_digest: '', consumer_digest: TackOpsDigests::CONSUMER_DIGEST },
     { server_digest: TackOpsDigests::SERVER_DIGEST, consumer_digest: '' }].each do |digests|
      result = described_class.run(**digests)

      expect(result.output).to include(TackOpsDigests::REQUEST_TASK, TackOpsDigests::REQUEST_MESSAGE), digests.inspect
      expect(result.output).not_to include(TackOpsDigests::VERIFY_TASK), digests.inspect
    end
  end

  it 'refuses a malformed digest before the verify task', :aggregate_failures do
    ["sha256:#{'A' * 64}", "sha512:#{'a' * 64}", "sha256:#{'a' * 63}", 'a' * 64,
     "sha256:#{'a' * 64};id"].each do |digest|
      result = described_class.run(server_digest: digest, consumer_digest: TackOpsDigests::CONSUMER_DIGEST)

      expect(result.output).to include(TackOpsDigests::REQUEST_MESSAGE), digest
      expect(result.output).not_to include(TackOpsDigests::VERIFY_TASK), digest
    end
  end
end
