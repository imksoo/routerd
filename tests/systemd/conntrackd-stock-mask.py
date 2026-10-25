import pathlib,subprocess,json,uuid
token='routerd-ct-test-'+uuid.uuid4().hex[:10]
stock=token+'-stock.service';managed=token+'-managed.service'
vendor=pathlib.Path.home()/'.local/share/systemd/user'
config=pathlib.Path.home()/'.config/systemd/user'
vendor.mkdir(parents=True,exist_ok=True);config.mkdir(parents=True,exist_ok=True)
paths=[vendor/stock,config/managed,config/stock]
assert not any(p.exists() or p.is_symlink() for p in paths)
records=[]
def run(*args,ok=True):
 p=subprocess.run(['systemctl','--user',*args],capture_output=True,text=True,timeout=15)
 records.append({'args':args,'rc':p.returncode,'out':p.stdout,'err':p.stderr})
 if ok:assert p.returncode==0,records[-1]
 return p
try:
 paths[0].write_text('[Unit]\nDescription=Isolated stock test\n[Service]\nExecStart=/usr/bin/sleep infinity\n')
 paths[1].write_text('[Unit]\nDescription=Isolated managed test\nConflicts='+stock+'\n[Service]\nExecStart=/usr/bin/sleep infinity\n')
 run('daemon-reload');run('start',managed)
 run('restart',stock)
 assert run('is-active',managed,ok=False).returncode!=0
 run('mask','--now',stock)
 run('start',managed)
 before=run('show',managed,'--property=MainPID','--value').stdout.strip()
 assert int(before)>0
 assert run('restart',stock,ok=False).returncode!=0
 after=run('show',managed,'--property=MainPID','--value').stdout.strip()
 assert before==after
 run('is-active',managed)
 print(json.dumps({'passed':True,'managed_pid_before':before,'managed_pid_after':after,'records':records}))
finally:
 run('stop',managed,stock,ok=False)
 run('unmask',stock,ok=False)
 for p in paths:
  if p.exists() or p.is_symlink():p.unlink()
 run('daemon-reload');run('reset-failed',managed,stock,ok=False)
