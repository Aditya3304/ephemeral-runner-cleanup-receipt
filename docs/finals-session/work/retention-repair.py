import json,pathlib,subprocess,datetime
root=pathlib.Path(__file__).resolve().parent.parent
cli=['bash',str(root/'work/aws.sh')]
bucket='cleanup-evidence-385020093237-us-east-1'
config={'ObjectLockEnabled':'Enabled','Rule':{'DefaultRetention':{'Mode':'COMPLIANCE','Days':8}}}
subprocess.run(cli+['s3api','put-object-lock-configuration','--bucket',bucket,'--object-lock-configuration',json.dumps(config)],check=True)
versions=json.loads(subprocess.check_output(cli+['s3api','list-object-versions','--bucket',bucket],text=True)).get('Versions',[])
until=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=8)).isoformat()
for v in versions:
    if not v['Key'].startswith('evidence/sha256/'):raise SystemExit('Unexpected key: stop')
    subprocess.run(cli+['s3api','put-object-retention','--bucket',bucket,'--key',v['Key'],'--version-id',v['VersionId'],'--retention',json.dumps({'Mode':'COMPLIANCE','RetainUntilDate':until})],check=True)
print('Applied eight-day default and extended',len(versions),'existing demo versions; strict seven-day verifier unchanged.')
