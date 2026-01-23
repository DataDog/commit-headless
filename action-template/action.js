const childProcess = require('child_process')
const fs = require('fs')
const os = require('os')
const process = require('process')

function chooseBinary() {
  const platform = os.platform()
  const arch = os.arch()

  if (platform === 'linux' && arch === 'x64') {
    return `dist/commit-headless-linux-amd64`
  }
  if (platform === 'linux' && arch === 'arm64') {
    return `dist/commit-headless-linux-arm64`
  }

  console.error(`Unsupported platform (${platform}) and architecture (${arch})`)
  process.exit(1)
}

function main() {
  const binary = chooseBinary()

  const cmd = `${__dirname}/${binary}`

  try {
    fs.chmodSync(cmd, 0o755);
  } catch (err) {
    console.error(`Error making binary executable: ${err.message}`);
  }

  const env = { ...process.env };
  env.HEADLESS_TOKEN = process.env.INPUT_TOKEN;

  const command = process.env.INPUT_COMMAND;

  if (!["commit", "push"].includes(command)) {
    console.error(`Unknown command '${command}'. Must be one of "commit" or "push".`);
    process.exit(1);
  }

  let args = [
    command,
    "--target", process.env.INPUT_TARGET,
    "--branch", process.env.INPUT_BRANCH
  ];

  const headSha = process.env["INPUT_HEAD-SHA"] || "";
  if (headSha !== "") {
    args.push("--head-sha", headSha);
  }

  const createBranch = process.env["INPUT_CREATE-BRANCH"] || "false"
  if(!["true", "false"].includes(createBranch.toLowerCase())) {
    console.error(`Invalid value for create-branch (${createBranch}). Must be one of true or false.`);
    process.exit(1);
  }

  if(createBranch.toLowerCase() === "true") { args.push("--create-branch") }

  const dryrun = process.env["INPUT_DRY-RUN"] || "false"
  if(!["true", "false"].includes(dryrun.toLowerCase())) {
    console.error(`Invalid value for dry-run (${dryrun}). Must be one of true or false.`);
    process.exit(1);
  }

  if(dryrun.toLowerCase() === "true") { args.push("--dry-run") }

  if (command === "commit") {
    const author = process.env["INPUT_AUTHOR"] || "";
    const message = process.env["INPUT_MESSAGE"] || "";
    if(author !== "") { args.push("--author", author) }
    if(message !== "") { args.push("--message", message) }
  }

  // The Go binary handles GITHUB_OUTPUT directly and uses stdout for logs
  // with workflow commands (grouping, notices, etc.)
  const child = childProcess.spawnSync(cmd, args, {
    env: env,
    cwd: process.env["INPUT_WORKING-DIRECTORY"] || process.cwd(),
    stdio: 'inherit',
  })

  if (child.error) {
    console.error(`Failed to run commit-headless: ${child.error.message}`);
    process.exit(1);
  }

  process.exit(child.status || 0);
}

if (require.main === module) {
  try {
    main()
  } catch (exc) {
    console.error(`Unhandled exception running action, got: ${exc.message}`);
    process.exit(1);
  }
}
