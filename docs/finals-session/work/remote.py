import json, pathlib, subprocess, sys, time
root=pathlib.Path(__file__).resolve().parent.parent
def aws(*args):
    return json.loads(subprocess.check_output(['bash',str(root/'work/aws.sh'),*args],text=True))
instance=next(x['OutputValue'] for x in json.loads((root/'work/stack-outputs.json').read_text()) if x['OutputKey']=='Host')
if sys.argv[1]=='poll':
    command=(root/'work/remote-command').read_text().strip()
else:
    script=pathlib.Path(sys.argv[1]).read_text()
    payload={'DocumentName':'AWS-RunShellScript','InstanceIds':[instance],'TimeoutSeconds':60,'Parameters':{'commands':[script],'executionTimeout':['7200']}}
    (root/'work/remote-input.json').write_text(json.dumps(payload))
    command=aws('ssm','send-command','--cli-input-json','file://'+str(root/'work/remote-input.json'))['Command']['CommandId']
    (root/'work/remote-command').write_text(command)
    print('Command',command)
    time.sleep(2)
try:
    result=aws('ssm','get-command-invocation','--command-id',command,'--instance-id',instance)
    print(result['Status']); print(result.get('StandardOutputContent','')[-10000:]);print(result.get('StandardErrorContent','')[-5000:])
except subprocess.CalledProcessError: print('Result pending')
