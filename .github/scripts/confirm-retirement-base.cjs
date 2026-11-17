module.exports = async ({github, context, core}) => {
  const reviewed = context.payload.pull_request;
  const {data: current} = await github.rest.pulls.get({
    ...context.repo,
    pull_number: reviewed.number,
  });
  if (reviewed.base.ref !== 'main' || current.base.ref !== 'main') {
    core.setFailed('Retirement verification requires the protected main branch as the current base.');
    return;
  }
  if (current.head.sha !== reviewed.head.sha || current.base.sha !== reviewed.base.sha) {
    core.setFailed('The pull request changed before verification. Run retirement again.');
  }
};
