// A real disposable workload. All credentials/resources created here are test-only.
import fs from 'node:fs';
import https from 'node:https';
import net from 'node:net';
import dgram from 'node:dgram';
import assert from 'node:assert/strict';

const mode=process.argv[2]??'pass';
const base='/var/run/secrets/kubernetes.io/serviceaccount/';
const token=fs.readFileSync(base+'token','utf8');
const ca=fs.readFileSync(base+'ca.crt');
function api(method,path,body){return new Promise((resolve,reject)=>{
 const request=https.request({host:process.env.KUBERNETES_SERVICE_HOST,port:443,path,method,ca,headers:{Authorization:`Bearer ${token}`,'Content-Type':method==='PATCH'?'application/merge-patch+json':'application/json'},timeout:5000},res=>{
  let data='';res.on('data',chunk=>{data+=chunk;if(data.length>65536)res.destroy(new Error('response too large'))});res.on('end',()=>resolve({status:res.statusCode,data}));
 });request.on('error',reject);request.on('timeout',()=>request.destroy(new Error('API timeout')));request.end(body?JSON.stringify(body):undefined);
})}
function connect(host,port){return new Promise(resolve=>{const socket=net.connect({host,port,timeout:1500});socket.once('connect',()=>{socket.destroy();resolve(true)});socket.once('timeout',()=>{socket.destroy();resolve(false)});socket.once('error',()=>resolve(false))})}
function udp(host,port){return new Promise(resolve=>{const socket=dgram.createSocket('udp4');const nonce=Buffer.from('proof-network-nonce');let done=false;const finish=v=>{if(done)return;done=true;clearTimeout(timer);socket.close();resolve(v)};const timer=setTimeout(()=>finish(false),1500);socket.on('message',b=>finish(b.equals(nonce)));socket.on('error',()=>finish(false));socket.send(nonce,port,host)})}

fs.writeFileSync(process.env.PROOF_WORKSPACE+'/.test-output','disposable');
fs.writeFileSync(process.env.PROOF_CREDENTIALS+'/test-token','dummy-local-value');
const ns=process.env.PROOF_NAMESPACE;
const created=await api('POST',`/api/v1/namespaces/${ns}/configmaps`,{apiVersion:'v1',kind:'ConfigMap',metadata:{name:'job-output'},data:{result:'local'}});
assert.equal(created.status,201,created.data);
console.log('LOCAL_JOB_STARTED '+mode);
if(mode==='isolation'){
 for(const [method,path,body] of [
  ['GET','/api/v1/namespaces/kube-system/secrets'],
  ['GET',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/pods`],
  ['PATCH',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/configmaps/assignment`,{}],
  ['PATCH',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/configmaps/another-run`,{}],
  ['PATCH',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/configmaps/runner-gate`,{data:{ready:'true'}}],
  ['PATCH',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/configmaps/guard-stage`,{metadata:{finalizers:['test.invalid/hold']}}],
  ['PATCH',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/configmaps/runner-gate-request`,{metadata:{labels:{'cleanup-receipt.local/run':'forged'}}}],
  ['POST',`/api/v1/namespaces/${ns}/pods`,{apiVersion:'v1',kind:'Pod',metadata:{name:'escape'},spec:{hostPID:true,containers:[{name:'escape',image:'fake',securityContext:{privileged:true}}]}}],
  ['POST',`/api/v1/namespaces/${ns}/persistentvolumeclaims`,{apiVersion:'v1',kind:'PersistentVolumeClaim',metadata:{name:'escape'},spec:{accessModes:['ReadWriteOnce'],resources:{requests:{storage:'1Mi'}}}}],
  ['POST',`/apis/rbac.authorization.k8s.io/v1/namespaces/${ns}/rolebindings`,{apiVersion:'rbac.authorization.k8s.io/v1',kind:'RoleBinding',metadata:{name:'escape'},roleRef:{apiGroup:'rbac.authorization.k8s.io',kind:'ClusterRole',name:'cluster-admin'},subjects:[]}],
  ['POST',`/api/v1/namespaces/${process.env.PROOF_STAGE_NAMESPACE}/serviceaccounts/guard/token`,{apiVersion:'authentication.k8s.io/v1',kind:'TokenRequest',spec:{audiences:['kubernetes'],expirationSeconds:600}}],
  ['DELETE','/api/v1/namespaces/default'],
 ]){const result=await api(method,path,body);assert.equal(result.status,403,`${method} ${path}: ${result.status}`)}
 assert.equal(fs.existsSync('/var/run/docker.sock'),false);
 assert.equal(fs.existsSync('/root/.kube/config'),false);
 for(const path of ['/run/auth/token','/run/archive','/run/admin','/run/finalizer','/run/secrets/aws'])assert.equal(fs.existsSync(path),false,`${path} unexpectedly mounted`);
 assert.throws(()=>fs.writeFileSync('/opt/guard/escape','x'));
 assert.equal(await connect('1.1.1.1',443),false,'external network reachable');
 for(const [host,port] of [['db',5432],['minio',9000],['issuer',8443],['fulcio',5555],['rekor',3000]])assert.equal(await connect(host,port),false,`${host}:${port} unexpectedly reachable`);
 // A fixed reachable local canary is supplied as argv by the integration harness.
 if(process.argv[3])assert.equal(await connect(process.argv[3],8080),false,'protected local canary reachable');
 if(process.argv[4]){
  const host=process.argv[4];
  assert.equal(await connect(host,20244),false,'node policy-controller port reachable');
  for(const port of [18080,31080]){
   assert.equal(await connect(host,port),false,`host TCP ${port} reachable`);
   assert.equal(await udp(host,port),false,`host UDP ${port} reachable`);
  }
 }
 console.log('ISOLATION_CHECKS_PASSED');
}
if(mode==='wait')await new Promise(resolve=>setTimeout(resolve,300000));
console.log('LOCAL_JOB_FINISHED '+mode);
if(mode==='fail')process.exitCode=7;
