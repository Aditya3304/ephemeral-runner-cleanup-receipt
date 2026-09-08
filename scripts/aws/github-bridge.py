#!/usr/bin/env python3
"""Run on the operator laptop through SSM port forwarding, never on the EC2 host.

The existing gh login stays here. A strict allowlist bounds the cloud's requested
GitHub operations. It cannot ask for tokens, secrets, repository writes or shell execution.
"""
import argparse,json,re,subprocess,time,urllib.request,urllib.error
PREFIX='/repos/Aditya3304/ephemeral-runner-cleanup-receipt'
def permitted(method,path,body,revision):
    if not path.startswith(PREFIX+'/'):return False
    p=path[len(PREFIX):]
    if method=='GET':return bool(re.fullmatch(r'/actions/(workflows/cleanup-aws\.yml/runs\?per_page=20|runs/[0-9]+/attempts/[0-9]+(?:/jobs\?per_page=100)?|jobs/[0-9]+|runners/[0-9]+)',p))
    if method=='DELETE':return bool(re.fullmatch(r'/actions/runners/[0-9]+',p))
    if method!='POST' or not isinstance(body,dict):return False
    if p=='/actions/runners/generate-jitconfig':
        return set(body)=={'name','runner_group_id','labels','work_folder'} and bool(re.fullmatch(r'proof-gh-[0-9]+-[0-9]+',body.get('name',''))) and body['labels']==[body['name']] and body['runner_group_id']==1 and body['work_folder']=='_work'
    return p=='/statuses/'+revision and set(body)=={'state','context','description'} and body['context']=='cleanup/signed-receipt' and body['state'] in ['pending','success','failure','error'] and len(body['description'])<=140
def exchange(url,body=None,token=None,method=None):
    data=None if body is None else json.dumps(body).encode()
    req=urllib.request.Request(url,data=data,method=method,headers={'Content-Type':'application/json','Accept':'application/vnd.github+json','User-Agent':'cleanup-local-bridge','X-GitHub-Api-Version':'2022-11-28'})
    if token:req.add_header('Authorization','Bearer '+token)
    try:
        with urllib.request.urlopen(req,timeout=30) as r:return r.status,json.loads(r.read(2*1024*1024) or b'{}')
    except urllib.error.HTTPError as e:return e.code,{}
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--revision',required=True);parser.add_argument('--port',type=int,default=18123);args=parser.parse_args()
    if not re.fullmatch('[0-9a-f]{40}',args.revision):raise SystemExit('Pinned source revision required')
    token=subprocess.check_output(['gh','auth','token'],text=True).strip()
    base=f'http://127.0.0.1:{args.port}'
    print('GitHub bridge running; administrative login remains on this laptop.',flush=True)
    while True:
        try:
            _,item=exchange(base+'/bridge/poll')
            if not item:continue
            method,path,body=item['method'],item['path'],item['body']
            status,response=403,{}
            if permitted(method,path,body,args.revision):
                safe=True
                if method=='DELETE':
                    s,r=exchange('https://api.github.com'+path,token=token)
                    safe=s==404 or (s==200 and re.fullmatch(r'proof-gh-[0-9]+-[0-9]+',r.get('name','')) and not r.get('busy',True))
                if safe:status,response=exchange('https://api.github.com'+path,body,token,method)
            exchange(base+'/bridge/response/'+item['id'],{'status':status,'body':response},method='POST')
        except (OSError,KeyError,ValueError):time.sleep(2)
if __name__=='__main__':main()
