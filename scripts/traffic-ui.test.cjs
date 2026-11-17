const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {test} = require('node:test');

const context = vm.createContext({
 Intl, Date,
 esc: value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
 icon: () => '<svg></svg>',
});
vm.runInContext(fs.readFileSync(require('node:path').join(__dirname,'../web/traffic.js'),'utf8'),context);

function fixture(){
 return {
  total: {requests:10, errors:2, client_errors:1, average_ms:125, max_ms:300},
  containers:[{name:'api',cpu_percent:12.5,memory_bytes:48*1048576,limit_bytes:512*1048576}],
  resource_status:'ok',sampled_at:'2026-11-17T12:00:00Z',interval_seconds:5,
  recent:[{at:'2026-11-17T12:00:00Z',method:'GET',route:'api.local/<script>',status:503,duration_ms:300}],
  buckets:Array.from({length:60},(_,i)=>({at:1794916500+i*5,requests:i===59?10:0,errors:i===59?2:0,average_ms:i===59?125:0,cpu_percent:i===59?12.5:null,memory_bytes:i===59?48*1048576:null})),
 };
}

test('renders Docker API memory byte fields as usage / limit, never NaN',()=>{
 const html=context.trafficContent(fixture());
 assert.match(html,/48\.0 MiB \/ 512\.0 MiB/);
 assert.match(html,/12\.5% CPU/);
 assert.doesNotMatch(html,/NaN|undefined/);
 assert.match(html,/20\.0%/);
 assert.match(html,/&lt;script&gt;/);
 assert.doesNotMatch(html,/<script>/);
});

test('missing container telemetry remains unavailable without inventing zero usage',()=>{
 const data=fixture();
 data.containers=[];data.resource_status='unavailable';
 data.buckets.forEach(b=>{b.cpu_percent=null;b.memory_bytes=null});
 const html=context.trafficContent(data);
 assert.match(html,/Container metrics unavailable/);
 assert.match(html,/Waiting for samples/);
 assert.doesNotMatch(html,/NaN|undefined|0\.0 MiB/);
 assert.match(html,/Response time/);
});
