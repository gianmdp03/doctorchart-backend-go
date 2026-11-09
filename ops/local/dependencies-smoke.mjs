// Run inside the instance's local-support container after browser-smoke.
import {readFileSync} from 'node:fs';
import net from 'node:net';
import tls from 'node:tls';
import {once} from 'node:events';
const settings=JSON.parse(readFileSync('/local/settings.json'));
const ready=await fetch('http://converter:8086/readyz');
if(!ready.ok)throw Error('Conversor no disponible');
const form=new FormData();
form.set('file',new Blob([readFileSync('/local/evidence/first-login.png')],{type:'image/png'}),'local-smoke.png');
form.set('profile','doctorchart-study');
const conversion=await fetch('http://converter:8086/v1/convert',{method:'POST',headers:{Authorization:`Bearer ${settings.image}`},body:form});
if(!conversion.ok)throw Error(`Conversión: ${conversion.status} ${await conversion.text()}`);
if(!(await conversion.arrayBuffer()).byteLength)throw Error('Conversión vacía');
console.log('Conversor real: PNG → perfil doctorchart-study OK');
if(process.argv.includes('--mail')){
 const socket=net.connect(1025,'mailpit');socket.setTimeout(5000,()=>socket.destroy(Error('SMTP timeout')));
 await once(socket,'connect');await once(socket,'data');
 socket.write('EHLO doctorchart.local\r\n');const [hello]=await once(socket,'data');
 if(!hello.toString().includes('STARTTLS'))throw Error('SMTP no anuncia STARTTLS');
 socket.write('STARTTLS\r\n');const [answer]=await once(socket,'data');
 if(!answer.toString().startsWith('220'))throw Error('SMTP rechaza STARTTLS');
 const secure=tls.connect({socket,servername:'mailpit',ca:readFileSync('/certs/cert.pem')});
 await once(secure,'secureConnect');
 if(!secure.authorized)throw Error('Certificado SMTP no verificado');
 secure.end('QUIT\r\n');
 console.log('Mailpit: STARTTLS y certificado SMTP verificados OK');
}
