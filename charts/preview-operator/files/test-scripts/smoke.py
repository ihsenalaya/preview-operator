import requests,sys,os
BASE=os.environ.get('APP_URL','http://app')
checks=[('/healthz',200),('/api/products',200)]
p,f=0,0
for path,code in checks:
    try:
        r=requests.get(BASE+path,timeout=5)
        ok=r.status_code==code
        label='PASS' if ok else 'FAIL'
        print(label+' smoke '+path+': '+str(r.status_code))
        p,f=(p+1,f) if ok else (p,f+1)
    except Exception as e:
        print('FAIL smoke '+path+': '+str(e))
        f+=1
print('Results: '+str(p)+' passed, '+str(f)+' failed')
sys.exit(1 if f>0 else 0)
