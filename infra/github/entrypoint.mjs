// Immutable one-job launcher. Administrative GitHub credentials never enter
// this container. The only registration material is its single-use JIT config.
import fs from 'node:fs/promises';
import {spawn} from 'node:child_process';
const runtime='/data/github-runtime';
let child;
let received;
for(const signal of ['SIGTERM','SIGINT']) process.on(signal,()=>{
  received=signal;
  if(child?.pid) { try { process.kill(-child.pid,signal); } catch {} }
});
async function run(file,args,options={}) {
  return await new Promise((resolve,reject)=>{
    child=spawn(file,args,{stdio:'inherit',detached:true,...options});
    child.once('error',reject);
    child.once('exit',(code,signal)=>resolve({code,signal}));
  });
}
try {
  await fs.mkdir(runtime,{mode:0o700}); // Must be new; no adoption of old state.
  await fs.cp('/opt/actions-runner',runtime,{recursive:true,force:false,errorOnExist:false});
  await fs.mkdir(runtime+'/tmp',{mode:0o700});
  const config=(await fs.readFile('/run/github-jit/config','utf8')).trim();
  if(!config || config.length>65536) throw new Error('Invalid single-use configuration');
  console.log('Starting the assigned one-job GitHub runner with externally observed CRI logs');
  const result=await run(runtime+'/bin/Runner.Listener',['run','--jitconfig',config],{
    cwd:runtime,env:{...process.env,HOME:runtime,TMPDIR:runtime+'/tmp',ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT:'1'}
  });
  // This is a job-side cleanup attempt, never trusted proof. The separate node
  // observer checks this exact directory after this container has terminated.
  await fs.rm(runtime,{recursive:true,force:false,maxRetries:2});
  console.log('GitHub listener exited; runtime cleanup attempted; awaiting independent observation');
  process.exitCode=received || result.signal ? 143 : (result.code ?? 1);
} catch(error) {
  console.error('GitHub runner lifecycle failed:',error instanceof Error ? error.message : 'unknown error');
  process.exitCode=1;
}
