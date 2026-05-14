import os,sys,urllib.request,urllib.parse,json
KEYCLOAK=os.environ['MICROCKS_KEYCLOAK_URL'].rstrip('/')
MICROCKS=os.environ['MICROCKS_URL'].rstrip('/')
CLIENT_ID=os.environ.get('MICROCKS_CLIENT_ID','microcks-serviceaccount')
SECRET=os.environ.get('MICROCKS_CLIENT_SECRET','ab54d329-e435-41ae-a900-ec6b3fe15c54')
USER=os.environ.get('MICROCKS_USERNAME','manager')
PASSWD=os.environ.get('MICROCKS_PASSWORD','microcks123')
SPEC_URL=os.environ['SPEC_URL']
print('Fetching spec from',SPEC_URL)
with urllib.request.urlopen(SPEC_URL,timeout=15) as r:
    spec_bytes=r.read()
print('Spec fetched:',len(spec_bytes),'bytes')
payload=urllib.parse.urlencode({'grant_type':'password','client_id':CLIENT_ID,'client_secret':SECRET,'username':USER,'password':PASSWD}).encode()
req=urllib.request.Request(KEYCLOAK+'/protocol/openid-connect/token',data=payload,method='POST',headers={'Content-Type':'application/x-www-form-urlencoded'})
with urllib.request.urlopen(req,timeout=10) as r:
    token=json.loads(r.read())['access_token']
print('Token obtained')
boundary=b'----MicrocksImport'
body=b'--'+boundary+b'\r\nContent-Disposition: form-data; name="file"; filename="openapi.yaml"\r\nContent-Type: application/yaml\r\n\r\n'+spec_bytes+b'\r\n--'+boundary+b'--\r\n'
req2=urllib.request.Request(MICROCKS+'/api/artifact/upload?mainArtifact=true',data=body,method='POST',headers={'Authorization':'Bearer '+token,'Content-Type':'multipart/form-data; boundary='+boundary.decode()})
try:
    with urllib.request.urlopen(req2,timeout=30) as r:
        raw=r.read()
        try:
            result=json.loads(raw)
            names=[s.get('name','')+':'+s.get('version','') for s in result] if isinstance(result,list) else [str(result)]
            print('Microcks import OK:',', '.join(names))
        except (json.JSONDecodeError,ValueError):
            print('Microcks import OK (status',r.status,')')
except urllib.error.HTTPError as e:
    print('Microcks import error:',e.code,e.read().decode()[:300],file=sys.stderr)
    sys.exit(1)
