import base64,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent.parent
target=root/'outputs/aws-validation';target.mkdir(exist_ok=True)
cli=['bash',str(root/'work/aws.sh')]
def output(command):
    result=json.loads(subprocess.check_output(cli+['ssm','get-command-invocation','--command-id',command,'--instance-id','i-0f21b25d3a04d8541'],text=True))
    assert result['Status']=='Success'
    return json.loads(result['StandardOutputContent'])
trust=output('53f7a966-4408-4a4e-a8b0-0dbc26bf5f37')
for name,value in trust.items():
    assert name in ['trusted-root.json','signing-config.json','issuer-ca.crt']
    (target/name).write_bytes(base64.b64decode(value,validate=True))
checks=output('8ba35436-0f76-4855-aac0-d414a88e0fbb')
(target/'aws-boundary-checks.json').write_text(json.dumps(checks,indent=2))
print('Saved public trust and actual AWS boundary-check results.')
