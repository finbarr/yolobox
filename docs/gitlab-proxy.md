# GitLab proxy

Use `--gitlab-proxy HOST/GROUP/PROJECT` to let `glab` read one GitLab project using your host login. The proxy accepts nested groups, such as `gitlab.example.com/platform/tools/cli`, and connects to that host over HTTPS. The container receives a separate credential that expires when the Yolobox command ends.

```bash
yolobox run --gitlab-proxy gitlab.com/my-team/my-project glab issue list
yolobox run --gitlab-proxy gitlab.com/my-team/my-project glab mr list
yolobox run --gitlab-proxy gitlab.com/my-team/my-project glab ci list
yolobox run --gitlab-proxy gitlab.com/my-team/my-project \
  glab api 'projects/my-team%2Fmy-project/merge_requests/42/diffs'
```

On the host, set `GITLAB_HOST` and `GITLAB_TOKEN` for the matching GitLab instance, or use a token readable by `glab config get token --global --host HOST`. The credential needs access to the project and a GitLab `read_api` or `api` scope. The proxy uses the host's normal HTTPS certificate trust.

The bridge allows GET requests for project metadata, issues and issue notes, merge requests with notes and diffs, and pipelines with their job lists. For REST requests, URL-encode `/` between the project path segments as `%2F`. Use `page` and `per_page` to retrieve additional results.

The scoped credential cannot access another project, submit changes, clone a repository, or call GraphQL. The proxy does not follow redirects. Existing Git access configured independently of the proxy still applies inside the box.
