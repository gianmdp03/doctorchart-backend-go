import {existsSync, readFileSync, writeFileSync} from 'node:fs';
import {execFileSync, spawn} from 'node:child_process';
import {createHash, X509Certificate} from 'node:crypto';
import {dirname, join, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

const root = join(process.env.USERPROFILE, 'Documents', 'DoctorChartManual');
const here = dirname(fileURLToPath(import.meta.url));
const driver = resolve(here, '../testing/specialty-m6-lab.mjs');
const [command = 'status', ...args] = process.argv.slice(2);
const run = (action, ...rest) => execFileSync(process.execPath, [driver, action, ...rest, '--manual'], {
  stdio: 'inherit', windowsHide: true,
});

if (command === 'start' || command === 'open') {
  if (!existsSync(join(root, 'compose.json')) || !existsSync(join(root, 'ACCESOS.txt'))) run('prepare');
  if (!['bin/backend', 'bin/gateway', 'web/browser/index.html'].every(path => existsSync(join(root, path)))) run('build');
  run('start');
  const launcher = fileURLToPath(import.meta.url);
  writeFileSync(join(root, 'Abrir DoctorChart Manual.cmd'),
    `@echo off\r\n"${process.execPath}" "${launcher}" open\r\nif errorlevel 1 pause\r\n`);
  if (command === 'open') {
    const chrome = [process.env.ProgramFiles, process.env['ProgramFiles(x86)'], process.env.LOCALAPPDATA]
      .filter(Boolean).map(path => join(path, 'Google/Chrome/Application/chrome.exe')).find(existsSync);
    if (!chrome) throw Error('Chrome no está disponible. El entorno quedó iniciado en https://localhost:4354/.');
    const cert = new X509Certificate(readFileSync(join(root, 'certs/cert.pem')));
    const pin = createHash('sha256').update(cert.publicKey.export({type: 'spki', format: 'der'})).digest('base64');
    spawn(chrome, ['--user-data-dir=' + join(root, 'Chrome'), '--ignore-certificate-errors-spki-list=' + pin,
      '--new-window', 'https://localhost:4354/login'], {detached: true, stdio: 'ignore'}).unref();
  }
} else if (['prepare', 'build', 'stop', 'status', 'verify', 'logs', 'restart', 'reset', 'credentials'].includes(command)) {
  run(command, ...args);
} else {
  throw Error('Comandos: start | open | stop | status | verify | logs [servicio] | restart servicio | reset --confirm-doctorchart-manual | credentials | build');
}
