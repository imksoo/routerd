"""Private evidence adapter. Missing observations never establish a violation or PASS."""
import json,math
from pathlib import Path
class ObservationInconclusive(RuntimeError):pass
class ProductFailure(RuntimeError):pass
CLASSES={'pass','product_failure','infra_failure','observation_inconclusive'}
def merge_classification(*values):
 for value in ('product_failure','infra_failure','observation_inconclusive'):
  if value in values:return value
 return 'pass'
def classify_assessment(checks,data):
 violations=[]
 # Positive, directly measured safety violations remain sticky across generations.
 for key in ('no_self_rejection','no_self_probe','no_confirmed_ping_failure'):
  if checks.get(key) is False and (key!='no_self_probe' or checks.get('self_probe_packet_parse',True)):
   violations.append(key)
 if checks.get('controller_success') is False and any(x.get('currentError') for row in data.get('receiver',{}).get('samples',[]) for x in row.get('controllers',[])):violations.append('controller reported error')
 if violations:return 'product_failure'
 if any(d.get('errors') or d.get('counterSampling',{}).get('errors') for d in data.values()):return 'infra_failure'
 return 'pass' if all(checks.values()) else 'observation_inconclusive'
def causal_packets(stimuli,kind,c,client,origin,cp,op,events,epoch):
 proofs=[];rejected=[]
 for s in stimuli:
  if s.get('case')!=kind:continue
  r=s.get('result',{});reason=None
  modern=any(k in s for k in ('stimulusId','senderRole')) or any(k in r for k in ('stimulusId','bootId'))
  if not modern:
   # Exact legacy conservative criterion, not a rescue path for malformed new records.
   try:
    lo=epoch(s['transport']['started_at']);hi=epoch(s['transport']['completed_at'])+10
    packets=[p for p in cp if lo<=p['at']-c['clockBounds']['client'] and p['at']+c['clockBounds']['client']<=hi]
    raw=[e for e in events if lo<=epoch(e['time'])-c['clockBounds']['origin'] and epoch(e['time'])+c['clockBounds']['origin']<=hi]
    if packets and op and raw and r.get('returncode')==0:
     proofs.append({'method':'legacy conservative QGA window with original packet/raw bounds','transport':s['transport'],'clientPackets':packets,'originPackets':op,'originRaw':raw})
    else:rejected.append({'reason':'legacy conservative evidence insufficient'})
   except (KeyError,ValueError,TypeError):rejected.append({'reason':'legacy evidence missing'})
   continue
  required=('startedEpoch','endedEpoch','sendMonotonic','endMonotonic','stimulusId','bootId')
  if any(k not in r for k in required):reason='missing original sender identity/clock record'
  elif r['stimulusId']!=s.get('stimulusId'):reason='stimulus identity mismatch'
  elif r.get('argv')!=s.get('argv') or not s.get('argv') or s['argv'][-1]!=(c['target'] if kind=='self' else c['positiveTarget']):reason='sender command binding mismatch'
  elif s.get('senderRole')!='client':reason='reverse ping requires outgoing ICMP causal evidence; not inferred from QGA'
  elif r.get('returncode')!=0 or not r.get('success'):reason='stimulus did not complete'
  elif not all(math.isfinite(r[k]) for k in required[:4]) or r['endedEpoch']<r['startedEpoch'] or r['endMonotonic']<r['sendMonotonic']:reason='invalid sender clocks'
  elif not client.get('samples') or any(x['bootId']!=r['bootId'] for x in client['samples']):reason='sender/capture boot mismatch'
  if reason:rejected.append({'stimulusId':s.get('stimulusId'),'reason':reason});continue
  # ARP has no nonce. Accept only uniquely attributable local command windows.
  for packet in cp:
   if not r['startedEpoch']<=packet['at']<=r['endedEpoch']:continue
   overlaps=[x for x in stimuli if x is not s and x.get('senderRole')=='client' and x.get('case')==kind and x.get('result',{}).get('startedEpoch',float('inf'))<=packet['at']<=x.get('result',{}).get('endedEpoch',float('-inf'))]
   if overlaps:continue
   matches=[q for q in op if all(q.get(k)==packet.get(k) for k in ('src','dst','target','sender')) and abs(q['at']-packet['at'])<=c['clockBounds']['client']+c['clockBounds']['origin']]
   raw=[e for e in events if any(abs(epoch(e['time'])-q['at'])<1.1 for q in matches)]
   if matches and raw:proofs.append({'stimulusId':r['stimulusId'],'sender':r,'clientPacket':packet,'originPackets':matches,'originRaw':raw,'method':'same-host sender interval and structurally matched ARP/origin raw; no coordinator wallclock freshness'})
 return {'success':bool(proofs),'proofs':proofs,'rejected':rejected}
def command_outcome(command,returncode,timed_out=False):
 if timed_out:return False,'infra_failure'
 path=command.get('evidenceResult')
 if path:
  try:
   r=json.loads(Path(path).read_text());cl=r['classification']
   if cl not in CLASSES or not isinstance(r['success'],bool):raise ValueError('invalid structured outcome')
   if r['success']!=(cl=='pass') or r['success']!=(returncode==0):raise ValueError('exit/structured result mismatch')
   return r['success'],cl
  except (OSError,ValueError,KeyError,TypeError):return False,'infra_failure'
 if any('selfprobe-test.py' in str(x) for x in command.get('argv',[])):return False,'infra_failure'
 return returncode==0, 'pass' if returncode==0 else command.get('classification','infra_failure')
