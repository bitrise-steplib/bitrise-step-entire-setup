# Entire setup step

Installs Entire's `git-remote-entire` helper so the Git Clone step can clone `entire://` repositories.

Repositories on [Entire](https://entire.com) are cloned through Entire's git remote helper: git runs
`git-remote-entire` for `entire://` URLs, and the helper authenticates with the checkout token Bitrise
puts into `ENTIRE_TOKEN` for the builds of an Entire project. Plain HTTPS cloning does not work for
private Entire repositories, so the helper has to be on `PATH` before the clone.

The step downloads the pinned [entireio/cli release](https://github.com/entireio/cli/releases),
verifies the archive against the release's `checksums.txt`, installs only `git-remote-entire` into
`install_dir` (default `$HOME/.local/bin`) and prepends that directory to `PATH` for the steps that
follow. It fails before downloading anything when `ENTIRE_TOKEN` is empty, and it never passes the
token anywhere: the helper reads it from the environment itself.

## Usage

Put the step right before Git Clone. Until the step is published to the steplib, reference it by its git URL:

```yaml
workflows:
  primary:
    steps:
    - git::https://github.com/bitrise-steplib/step-entire-setup.git@ci-6575-entire-setup: {}
    - git-clone@8: {}
```

Nothing else changes: the Git Clone step clones the `entire://` repository URL as it is, with git finding
the helper on `PATH`.

## Inputs

| Input | Default | Purpose |
| --- | --- | --- |
| `entire_version` | `0.12.2` | The entireio/cli release to install the helper from |
| `install_dir` | `$HOME/.local/bin` | Where the helper is written; prepended to `PATH` |
| `entire_token` | `$ENTIRE_TOKEN` | Checked for presence only |
| `verbose` | `false` | Debug logging |

Output: `ENTIRE_SETUP_HELPER_PATH`, the installed helper's path.

## Development

```bash
bitrise run test
go test ./...
```
