import json,pathlib,subprocess,urllib.request,hashlib
root=pathlib.Path(__file__).resolve().parent.parent
target=root/'outputs/aws-validation';target.mkdir(exist_ok=True)
with urllib.request.urlopen('http://127.0.0.1:18080/v1/receipts?limit=20') as response:page=json.load(response)
(target/'verified-api-records.json').write_text(json.dumps(page,indent=2))
cli=['bash',str(root/'work/aws.sh')]
for row in page['items']:
    folder=target/(row['run_id']+'-'+str(row['run_attempt']));folder.mkdir(exist_ok=True)
    for key,name in [('receipt_object','receipt.json'),('bundle_object','receipt.bundle.json')]:
        ref=row[key]
        subprocess.run(cli+['s3api','get-object','--bucket',ref['bucket'],'--key',ref['key'],'--version-id',ref['version_id'],str(folder/name)],check=True,stdout=subprocess.DEVNULL)
        if hashlib.sha256((folder/name).read_bytes()).hexdigest()!=ref['sha256']:raise SystemExit('Export digest mismatch')
    print('Exported exact S3 versions:',row['run_id'],row['verdict'])
