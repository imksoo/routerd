#!/usr/bin/env python3
'Run actual bootstrap with mock tools and no root, cloud, or downloads.'
from pathlib import Path
import os,subprocess,tempfile,unittest
ROOT=Path(__file__).resolve().parents[4]
SCRIPT=ROOT/'tools/release-qa-labs/terraform/modules/cli-bootstrap.sh'
class BootstrapTests(unittest.TestCase):
 def run_bootstrap(self,provider,mode):
  with tempfile.TemporaryDirectory() as tmp:
   base=Path(tmp);bindir=base/'bin';bindir.mkdir()
   def command(name,body):
    p=bindir/name;p.write_text('#!/bin/bash\nset -e\n'+body+'\n');p.chmod(0o755)
   command('env','while [[ "$1" == *=* ]]; do shift; done\nexec "$@"')
   command('sleep','exit 0')
   command('mktemp','exec /usr/bin/mktemp -d "$CASE_TMP/work.XXXXXXXX"')
   command('apt-get',r'''
echo "apt $*" >>"$TRACE"
if [ "$MODE" = lock ] && [ ! -e "$CASE_TMP/lock-once" ]; then
 touch "$CASE_TMP/lock-once"; echo "Could not get lock" >&2; exit 100
fi
if [ "$MODE" = package ]; then echo "Unable to locate package" >&2; exit 100; fi
''')
   installer=r'''
echo "installer $*" >>"$TRACE"
n=$(cat "$CASE_TMP/install-count" 2>/dev/null || echo 0);n=$((n+1));echo "$n" >"$CASE_TMP/install-count"
if [ "$MODE" = exhausted ] || { [ "$MODE" = transient ] && [ "$n" = 1 ]; }; then echo "Temporary failure in name resolution" >&2; exit 1; fi
if [ "$MODE" = permanent ]; then echo "Permission denied" >&2; exit 1; fi
echo installed
'''
   (base/'installer').write_text('#!/bin/bash\nset -e\n'+installer);(base/'installer').chmod(0o755)
   command('curl',r'''
echo "curl $*" >>"$TRACE"
if [ "$MODE" = download ]; then echo "The requested URL returned error: 404" >&2; exit 22; fi
for out in "$@"; do :; done
cp "$CASE_TMP/installer" "$out"
''')
   command('unzip',r'''
echo "unzip $*" >>"$TRACE"
for out in "$@"; do :; done
mkdir -p "$out/aws";cp "$CASE_TMP/installer" "$out/aws/install"
''')
   for cli in ['aws','az','oci']:
    command(cli,'echo "version '+cli+' $*" >>"$TRACE"\necho synthetic-version')
   env=dict(os.environ,PATH=str(bindir)+':/usr/bin:/bin',CASE_TMP=tmp,TRACE=str(base/'trace'),MODE=mode,ROUTERD_LAB_BOOTSTRAP_LOG_DIR=str(base/'logs'))
   p=subprocess.run(['bash',str(SCRIPT),provider],cwd=tmp,env=env,text=True,capture_output=True)
   trace=(base/'trace').read_text() if (base/'trace').exists() else ''
   logs={x.name:x.read_text() for x in (base/'logs').glob('*')}
   self.assertFalse(list(base.glob('work.*')),'owned temp leaked')
   return p,trace,logs
 def test_normal_all_providers(self):
  for provider in ['aws','azure','oci']:
   with self.subTest(provider=provider):
    p,t,l=self.run_bootstrap(provider,'success');self.assertEqual(p.returncode,0,p.stderr)
    self.assertEqual(sum(x.startswith('curl ') for x in t.splitlines()),1);self.assertEqual(t.count('installer '),1)
    self.assertIn('--retry 5 --retry-delay 2 --retry-connrefused',t);self.assertNotIn('--retry-all-errors',t)
    self.assertIn('exit=0',l[provider+'-version.attempt-1.result.txt'])
 def test_transient_install_retries_without_redownload(self):
  for provider in ['azure','oci']:
   with self.subTest(provider=provider):
    p,t,l=self.run_bootstrap(provider,'transient');self.assertEqual(p.returncode,0,p.stderr)
    self.assertEqual(sum(x.startswith('curl ') for x in t.splitlines()),1);self.assertEqual(t.count('installer '),2)
    self.assertIn('exit=1',l[provider+'-install.attempt-1.result.txt'])
    self.assertIn('Temporary failure',l[provider+'-install.attempt-1.stderr'])
    self.assertIn('exit=0',l[provider+'-install.attempt-2.result.txt'])
 def test_transient_installer_exhaustion_preserves_every_attempt(self):
  for provider in ['azure','oci']:
   with self.subTest(provider=provider):
    p,t,l=self.run_bootstrap(provider,'exhausted');self.assertEqual(p.returncode,1,p.stderr)
    self.assertEqual(t.count('installer '),3)
    self.assertEqual(sum(x.startswith('curl ') for x in t.splitlines()),1)
    self.assertNotIn('version ',t)
    for attempt in range(1,4):
     prefix=f'{provider}-install.attempt-{attempt}'
     self.assertIn('exit=1',l[prefix+'.result.txt'])
     self.assertIn('Temporary failure',l[prefix+'.stderr'])
    self.assertNotIn(provider+'-install.attempt-4.result.txt',l)
 def test_permanent_install_error_no_retry_or_version(self):
  for provider in ['azure','oci']:
   with self.subTest(provider=provider):
    p,t,l=self.run_bootstrap(provider,'permanent');self.assertEqual(p.returncode,1)
    self.assertEqual(t.count('installer '),1);self.assertNotIn('version ',t)
    self.assertIn('Permission denied',l[provider+'-install.attempt-1.stderr'])
 def test_missing_package_not_retried(self):
  p,t,l=self.run_bootstrap('oci','package');self.assertEqual(p.returncode,100)
  self.assertEqual(t.count('apt '),1);self.assertNotIn('curl ',t)
  self.assertIn('exit=100',l['prerequisites-update.attempt-1.result.txt'])
 def test_package_lock_retries_failed_operation_only(self):
  p,t,l=self.run_bootstrap('oci','lock');self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual(t.count('apt '),3);self.assertEqual(sum(x.startswith('curl ') for x in t.splitlines()),1)
  self.assertIn('exit=100',l['prerequisites-update.attempt-1.result.txt'])
  self.assertIn('exit=0',l['prerequisites-update.attempt-2.result.txt'])
 def test_fatal_download_never_installs(self):
  for provider in ['aws','azure','oci']:
   with self.subTest(provider=provider):
    p,t,l=self.run_bootstrap(provider,'download');self.assertEqual(p.returncode,22,p.stderr)
    self.assertEqual(sum(x.startswith('curl ') for x in t.splitlines()),1);self.assertNotIn('installer ',t);self.assertNotIn('version ',t)
    self.assertIn('exit=22',l[provider+'-download.attempt-1.result.txt'])
if __name__=='__main__':unittest.main()
