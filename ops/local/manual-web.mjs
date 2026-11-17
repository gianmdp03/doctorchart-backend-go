import {createServer as httpsServer, request as httpsRequest} from 'node:https';
import {request as httpRequest} from 'node:http';
import {readFileSync} from 'node:fs';
import {createPreview} from './preview-server.mjs';

const cert = readFileSync('/certs/cert.pem');
const key = readFileSync('/certs/key.pem');
const site = JSON.parse(readFileSync('/site-config.json', 'utf8'));
const preview = createPreview({...site, directory: '/site'});

httpsServer({cert, key}, (req, res) => {
  res.setHeader('X-Robots-Tag', 'noindex, nofollow');
  const auth = req.url.startsWith('/auth/v1/');
  if (!auth && !req.url.startsWith('/api/')) {
    preview.emit('request', req, res);
    return;
  }
  const upstream = (auth ? httpsRequest : httpRequest)({
    hostname: auth ? 'local-support' : 'gateway',
    port: auth ? 9443 : 8000,
    path: req.url,
    method: req.method,
    headers: req.headers,
    ...(auth ? {ca: cert} : {}),
  }, reply => {
    res.writeHead(reply.statusCode, reply.headers);
    reply.pipe(res);
  });
  upstream.setTimeout(60000, () => upstream.destroy());
  upstream.on('error', () => {
    if (!res.headersSent) res.writeHead(502);
    res.end();
  });
  req.pipe(upstream);
}).listen(4354, '0.0.0.0');
