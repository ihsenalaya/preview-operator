import sys,os,json,time
try:
    import urllib.request,urllib.error,urllib.parse
except ImportError:
    print("FAIL contract: urllib not available")
    sys.exit(1)
MICROCKS_URL=os.environ.get('MICROCKS_URL','').rstrip('/')
BACKEND_URL=os.environ.get('BACKEND_URL','')
API_NAME=os.environ.get('API_NAME','Preview Catalog API')
API_VERSION=os.environ.get('API_VERSION','1.0.0')
TEST_RUNNER=os.environ.get('TEST_RUNNER','OPEN_API_SCHEMA')
TIMEOUT_MS=int(os.environ.get('TEST_TIMEOUT_MS','60000'))
CLIENT_ID=os.environ.get('MICROCKS_CLIENT_ID','')
CLIENT_SECRET=os.environ.get('MICROCKS_CLIENT_SECRET','')
KEYCLOAK_URL=os.environ.get('MICROCKS_KEYCLOAK_URL','')
def http_json(url,method='GET',data=None,hdrs={}):
    body=json.dumps(data).encode() if data else None
    req=urllib.request.Request(url,data=body,method=method,headers={'Content-Type':'application/json','Accept':'application/json',**hdrs})
    try:
        with urllib.request.urlopen(req,timeout=15) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        raise Exception('HTTP '+str(e.code)+': '+e.read().decode()[:200])
token=''
if CLIENT_ID and CLIENT_SECRET and KEYCLOAK_URL:
    try:
        data=urllib.parse.urlencode({'grant_type':'client_credentials','client_id':CLIENT_ID,'client_secret':CLIENT_SECRET}).encode()
        req=urllib.request.Request(KEYCLOAK_URL,data=data,method='POST',headers={'Content-Type':'application/x-www-form-urlencoded'})
        with urllib.request.urlopen(req,timeout=10) as r:
            token=json.loads(r.read()).get('access_token','')
        print('  auth: token obtained')
    except Exception as e:
        print('  WARN auth: '+str(e))
auth={'Authorization':'Bearer '+token} if token else {}
try:
    payload={'serviceId':API_NAME+':'+API_VERSION,'testEndpoint':BACKEND_URL,'runnerType':TEST_RUNNER,'timeout':TIMEOUT_MS}
    result=http_json(MICROCKS_URL+'/api/tests','POST',payload,auth)
    test_id=result.get('id','')
    if not test_id:
        print('FAIL contract: no test id from Microcks')
        sys.exit(1)
    print('  submitted test '+test_id)
except Exception as e:
    print('FAIL contract: submit failed: '+str(e))
    sys.exit(1)
max_poll=int(TIMEOUT_MS/1000/5)+6
for i in range(max_poll):
    time.sleep(5)
    try:
        result=http_json(MICROCKS_URL+'/api/tests/'+test_id,hdrs=auth)
    except Exception as e:
        print('  WARN poll: '+str(e))
        continue
    if result.get('inProgress',True):
        continue
    p,f=0,0
    for tc in result.get('testCaseResults',[]):
        op=tc.get('operationName','?')
        for step in tc.get('testStepResults',[]):
            if step.get('success',False):
                print('PASS contract '+op)
                p+=1
            else:
                print('FAIL contract '+op+': '+step.get('message','schema violation'))
                f+=1
    print('Results: '+str(p)+' passed, '+str(f)+' failed')
    sys.exit(1 if f>0 else 0)
print('FAIL contract: timeout')
sys.exit(1)
