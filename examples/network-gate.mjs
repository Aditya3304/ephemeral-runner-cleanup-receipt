// Trusted immutable init container: no user command starts before the coordinator
// installs and verifies the firewall in this Pod's own network namespace.
import fs from 'node:fs';
import https from 'node:https';
import {randomBytes} from 'node:crypto';
import {setTimeout as delay} from 'node:timers/promises';
const root='/var/run/secrets/kubernetes.io/serviceaccount/';
const ns=process.env.PROOF_STAGE_NAMESPACE;
const podUID=process.env.PROOF_POD_UID;
if(!/^[a-z0-9-]+$/.test(ns??'')||!podUID)throw new Error('Missing gate identity');
const nonce=randomBytes(32).toString('hex');
let requested=false;
const deadline=Date.now()+150000;
while(Date.now()<deadline){
 try{
  if(!requested){
   await new Promise((resolve,reject)=>{
    const payload=JSON.stringify({data:{nonce,pod_uid:podUID}});
    const req=https.request({host:process.env.KUBERNETES_SERVICE_HOST,port:443,path:`/api/v1/namespaces/${ns}/configmaps/runner-gate-request`,
     method:'PATCH',ca:fs.readFileSync(root+'ca.crt'),headers:{Authorization:'Bearer '+fs.readFileSync(root+'token','utf8').trim(),'Content-Type':'application/merge-patch+json'},timeout:3000},res=>{
      res.resume();res.on('end',()=>res.statusCode===200?resolve():reject(new Error('Gate request rejected')));res.on('error',reject);
     });req.on('error',reject);req.on('timeout',()=>req.destroy(new Error('Gate request timeout')));req.end(payload);
   });requested=true;
  }
  const body=await new Promise((resolve,reject)=>{
   const req=https.get({host:process.env.KUBERNETES_SERVICE_HOST,port:443,
    path:`/api/v1/namespaces/${ns}/configmaps/runner-gate`,ca:fs.readFileSync(root+'ca.crt'),
    headers:{Authorization:'Bearer '+fs.readFileSync(root+'token','utf8').trim()},timeout:3000},res=>{
    let data='';res.on('data',c=>{data+=c;if(data.length>8192)res.destroy(new Error('Gate response limit'))});
    res.on('end',()=>res.statusCode===200?resolve(JSON.parse(data)):reject(new Error('Gate unavailable')));res.on('error',reject);
   });req.on('error',reject);req.on('timeout',()=>req.destroy(new Error('Gate timeout')));
  });
  if(body.data?.ready==='true'&&body.data?.pod_uid===podUID&&body.data?.nonce===nonce){console.log('Network isolation installed for assigned Pod and init attempt');process.exit(0)}
 }catch{ /* No user command is running. Retry local API/readiness only. */ }
 await delay(500);
}
throw new Error('Network isolation gate did not open; user command was not started');
