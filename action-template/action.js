const childProcess = require('child_process')
const crypto = require('crypto')
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

  const env = { ...process.env };
  env.HEADLESS_TOKEN = process.env.INPUT_TOKEN;

  const command = process.env.INPUT_COMMAND;

  if (!["commit", "push"].includes(command)) {
    console.error(`Unknown command ${command}. Must be one of "commit" or "push".`);
    process.exit(1);
  }

  let args = [
    command,
    "--target", process.env.INPUT_TARGET,
    "--branch", process.env.INPUT_BRANCH
  ];

  const branchFrom = process.env["INPUT_BRANCH-FROM"] || "";
  if (branchFrom !== "") {
    args.push("--branch-from", branchFrom);
  }

  if (command === "push") {
    args.push(...process.env.INPUT_COMMITS.split(/\s+/));
  } else {
    const author = process.env["INPUT_AUTHOR"] || "";
    const message = process.env["INPUT_MESSAGE"] || "";
    if(author !== "") { args.push("--author", author) }
    if(message !== "") { args.push("--message", message) }

    const force = process.env["INPUT_FORCE"] || "false"
    if(!["true", "false"].includes(force.toLowerCase())) {
      console.error(`Invalid value for force (${force}). Must be one of true or false.`);
      process.exit(1);
    }

    if(force.toLowerCase() === "true") { args.push("--force") }

    args.push(...process.env.INPUT_FILES.split(/\s+/));
  }

  const child = childProcess.spawnSync(cmd, args, {
    env: env,
    stdio: ['ignore', 'pipe', 'inherit'],
  })

  const exitCode = child.status
  if (typeof exitCode === 'number') {
    if(exitCode === 0) {
      const out = child.stdout.toString().trim();
      console.log(`Pushed reference ${out}`);

      const delim = `delim_${crypto.randomUUID()}`;
      fs.appendFileSync(process.env.GITHUB_OUTPUT, `pushed_ref<<${delim}${os.EOL}${out}${os.EOL}${delim}`, { encoding: "utf8" });
    }

    process.exit(exitCode)
  }
  process.exit(1)
}

if (require.main === module) {
  main()
}
