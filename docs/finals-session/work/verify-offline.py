import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent.parent
target=root/'outputs/aws-validation'
cosign='/mnt/c/Users/Aditya/Documents/Codex/2026-09-07/i-need-you-to-fully-implement/outputs/ephemeral-runner-cleanup-receipt/.build/cosign'
checks=[]
for folder in sorted(p for p in target.iterdir() if p.is_dir()):
    receipt=folder/'receipt.json';bundle=folder/'receipt.bundle.json'
    if not receipt.exists():continue
    document=json.loads(receipt.read_bytes())
    assert hashlib.sha256((target/'trusted-root.json').read_bytes()).hexdigest()==document['finalizer']['trust_root_sha256']
    command=[cosign,'verify-blob','--offline','--trusted-root',str(target/'trusted-root.json'),'--bundle',str(bundle),'--certificate-identity','finalizer@cleanup-receipt.local','--certificate-oidc-issuer','https://issuer:8443']
    result=subprocess.run(command+[str(receipt)],capture_output=True,text=True)
    assert result.returncode==0,result.stderr
    tampered=root/'work/tampered-receipt.json';tampered.write_bytes(receipt.read_bytes()+b' ')
    invalid=subprocess.run(command+[str(tampered)],capture_output=True,text=True)
    assert invalid.returncode!=0,'Tampered signature unexpectedly accepted'
    checks.append({'run':folder.name,'verdict':document['verdict'],'receipt_sha256':hashlib.sha256(receipt.read_bytes()).hexdigest(),'offline_signature':'verified','tampered_bytes':'rejected'})
(target/'offline-verification.json').write_text(json.dumps(checks,indent=2))
print(json.dumps(checks,indent=2))
