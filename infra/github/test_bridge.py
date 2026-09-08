import importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('bridge',pathlib.Path(__file__).resolve().parents[2]/'scripts/aws/github-bridge.py')
bridge=importlib.util.module_from_spec(spec);spec.loader.exec_module(bridge)
class BridgeTests(unittest.TestCase):
    def test_operations_are_restricted(self):
        prefix=bridge.PREFIX;sha='a'*40
        self.assertTrue(bridge.permitted('GET',prefix+'/actions/runners?per_page=100',None,sha))
        for path in ['/user','/repos/other/repo/actions/runners/1',prefix+'/actions/secrets',prefix+'/contents/README.md']:
            self.assertFalse(bridge.permitted('GET',path,None,sha))
        body={'name':'proof-gh-123-1','labels':['proof-gh-123-1'],'runner_group_id':1,'work_folder':'_work'}
        self.assertTrue(bridge.permitted('POST',prefix+'/actions/runners/generate-jitconfig',body,sha))
        body['work_folder']='/etc'
        self.assertFalse(bridge.permitted('POST',prefix+'/actions/runners/generate-jitconfig',body,sha))
        status={'state':'success','context':'cleanup/signed-receipt','description':'verified'}
        self.assertTrue(bridge.permitted('POST',prefix+'/statuses/'+sha,status,sha))
        self.assertFalse(bridge.permitted('POST',prefix+'/statuses/'+'b'*40,status,sha))
if __name__=='__main__':unittest.main()
