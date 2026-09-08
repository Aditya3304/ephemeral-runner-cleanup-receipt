import json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/aws-validation'
page=json.loads((out/'verified-api-records.json').read_text())
row=next(x for x in page['items'] if x['run_id']=='34200085381' and x['run_attempt']==2)
results=[]
for kind in ['receipt_object','bundle_object']:
    ref=row[kind]
    result=json.loads(subprocess.check_output(['bash',str(root/'work/aws.sh'),'s3api','head-object','--bucket',ref['bucket'],'--key',ref['key'],'--version-id',ref['version_id']],text=True))
    assert result['ServerSideEncryption']=='aws:kms'
    assert result['ObjectLockMode']=='COMPLIANCE'
    assert result['VersionId']==ref['version_id']
    assert result['SSEKMSKeyId']=='arn:aws:kms:us-east-1:385020093237:key/b364db70-6485-488d-b59b-6790d4145987'
    results.append({'kind':kind,'reference':ref,'metadata':result})
(out/'s3-version-metadata.json').write_text(json.dumps(results,indent=2))
print('Final receipt and bundle: exact S3 versions, KMS key and Compliance retention confirmed.')
