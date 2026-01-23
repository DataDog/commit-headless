# Changelog

## v3.0.0

### Breaking Changes

- **push command no longer accepts commit arguments**: The push command now automatically
  determines which local commits need to be pushed by comparing local HEAD with the remote branch
  HEAD. Previously, you could specify which commits to push as arguments. If the remote HEAD is not
  an ancestor of local HEAD, the push will fail due to diverged history.

- **commit command no longer accepts file arguments**: The commit command now reads from staged
  changes (via `git add`), similar to how `git commit` works. Previously, you had to specify the
  list of files to include in the commit. Stage your changes first, then run the command.

- **Action inputs removed**:
  - `commits` input removed from push (commits are now auto-detected)
  - `files` input removed from commit (files are now read from staging area)

### Features

- **File mode preservation**: Executable bits and other file modes are now preserved when pushing
  commits. Previously all files were created with mode `100644`.

- **GitHub Actions logging**: When running in GitHub Actions, output now uses workflow commands for
  better integration:
  - Commit operations are grouped for cleaner logs
  - Success/failure notices appear in the workflow summary
  - Warnings and errors use appropriate annotation levels

- **REST API**: Switched from GraphQL API to REST API internally, enabling file mode support and
  improved error handling.

### Other Changes

- Added CI test workflow that runs integration tests on pull requests
- Release workflow now verifies binaries on both amd64 and arm64 before releasing
