const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const expectedNode = fs.readFileSync(path.join(root, '.node-version'), 'utf8').trim();
const expectedPnpm = require(path.join(root, 'package.json')).packageManager.split('@')[1];
if (process.versions.node !== expectedNode) {
  throw new Error(`Node ${expectedNode} required; found ${process.versions.node}`);
}
const agent = process.env.npm_config_user_agent;
const pnpm = agent ? agent.split(' ')[0] : `pnpm/${execFileSync('pnpm', ['--version'], { cwd: root, encoding: 'utf8' }).trim()}`;
if (pnpm !== `pnpm/${expectedPnpm}`) {
  throw new Error(`pnpm ${expectedPnpm} required; found ${pnpm}`);
}
