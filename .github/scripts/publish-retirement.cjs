module.exports = async ({github, context, core}) => {
  const reviewed = context.payload.pull_request;
  const {data: current} = await github.rest.pulls.get({
    ...context.repo,
    pull_number: reviewed.number,
  });
  if (reviewed.base.ref !== 'main' || current.base.ref !== 'main') {
    core.setFailed('Retirement publication requires the protected main branch as the current base.');
    return;
  }
  if (current.head.sha !== reviewed.head.sha || current.base.sha !== reviewed.base.sha) {
    core.setFailed('The pull request changed after verification. Run retirement again.');
    return;
  }
  const passed = process.env.VERDICT === 'success' && process.env.VERIFY_RESULT === 'success';
  await github.rest.repos.createCommitStatus({
    ...context.repo,
    sha: reviewed.head.sha,
    context: 'retirement',
    state: passed ? 'success' : 'failure',
    description: passed ? 'Retirement state verification passed.' : 'Retirement verification failed. Inspect the workflow run.',
    target_url: `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`,
  });
};
