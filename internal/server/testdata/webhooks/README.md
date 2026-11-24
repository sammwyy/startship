Trimmed from `@octokit/webhooks-examples@7.6.1`, `api.github.com/index.json`
(https://github.com/octokit/webhooks).

Examples: push[4], push[1] (tag deletion), pull_request[3], ping[0],
delete[0], create[0]. Only unrelated fields were removed.

The package captures branch creation, tag deletion, and unmerged PR closure;
it has no regular branch push, branch deletion or merged PR closure capture.
The Go test explicitly derives those variants by changing `created`/`before`,
the ref namespace or `ref_type`, and `merged`, respectively. The zero `after`
SHA and null `head_commit` on deletion are retained from the capture.
