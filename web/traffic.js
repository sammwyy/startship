'use strict';

// Kept separate from the dashboard navigation prototype in PR #12.
async function trafficPage(id) {
 const version=viewVersion, project=await api('projects/'+encodeURIComponent(id));
 if(version!==viewVersion)return;
 workspace=workspaces.find(w=>w.id===project.workspace_id);
 shell('<a href="#project/'+esc(project.id)+'">'+esc(project.name)+'</a> &nbsp; / &nbsp; Traffic',
  '<div class="traffic-layout"><aside class="traffic-sidebar"><a class="traffic-project" href="#project/'+esc(project.id)+'"><span class="card-icon">'+icon('box')+'</span><strong>'+esc(project.name)+'</strong></a><span class="traffic-kicker">PROJECT</span><nav aria-label="Project monitoring"><a href="#project/'+esc(project.id)+'">'+icon('grid')+'Project overview</a><a class="active" aria-current="page" href="#project/'+esc(project.id)+'/traffic">'+icon('activity')+'Traffic<span class="traffic-dot"></span></a></nav><div class="traffic-source">'+icon('branch')+'<span>'+esc(project.branch||'No source connected')+'</span><small>One project. One source.</small></div></aside>'+ 
  '<section class="traffic-main" aria-labelledby="traffic-title"><div class="traffic-heading"><div><div class="traffic-kicker">OBSERVABILITY / BUILT IN</div><h1 id="traffic-title">Live traffic<span class="traffic-prototype">Prototype</span></h1><p>Your router sees it. Now you do.</p></div><div class="traffic-controls"><span id="traffic-live" class="traffic-live" role="status">Connecting…</span><div class="traffic-ranges" aria-label="Traffic time range">'+[[300,'5m'],[900,'15m'],[3600,'1h']].map(([n,label])=>'<button data-window="'+n+'" aria-pressed="'+(n===300)+'">'+label+'</button>').join('')+'</div></div></div>'+ 
  '<div id="traffic-error" role="alert" hidden></div><div id="traffic-data"><div class="traffic-loading">Listening to your project…</div></div>'+ 
  '<div class="traffic-footnote"><span>'+icon('lock')+'No agents. No SDKs. Nothing to configure.</span><span>Last hour · in memory · resets on restart</span></div></section></div>');
 $('.dashboard').classList.add('traffic-shell');
 let windowSeconds=300,loading=false;
 const current=()=>version===viewVersion&&!!$('#traffic-data');
 async function refresh(){
  if(loading||!current()||document.hidden)return;
  loading=true;
  const requested=windowSeconds;
  try {
   const data=await api('projects/'+encodeURIComponent(id)+'/traffic?window='+requested);
   if(!current()||requested!==windowSeconds)return;
   $('#traffic-data').innerHTML=trafficContent(data);
   $('#traffic-error').hidden=true;
   $('#traffic-live').classList.remove('offline');
   $('#traffic-live').innerHTML='<span class="traffic-dot"></span>Live · 5s';
   $('#traffic-live').title='Updated '+new Date(data.now).toLocaleTimeString();
  } catch(error) {
   if(!current())return;
   $('#traffic-error').hidden=false;
   $('#traffic-error').textContent='Traffic could not refresh. '+error.message+' Retrying automatically.';
   $('#traffic-live').classList.add('offline');
   $('#traffic-live').textContent='Disconnected';
  } finally {loading=false}
 }
 document.querySelectorAll('[data-window]').forEach(button=>button.onclick=async()=>{
  windowSeconds=Number(button.dataset.window);
  document.querySelectorAll('[data-window]').forEach(b=>b.setAttribute('aria-pressed',String(b===button)));
  // Do not label old data with the newly selected range while a request is pending.
  $('#traffic-live').textContent='Updating…';
  if(!loading)await refresh();
 });
 await refresh();
 if(current())timer=setInterval(refresh,5000);
}

function trafficNumber(value){return new Intl.NumberFormat(undefined,{maximumFractionDigits:0}).format(value)}
function trafficMS(value){return value<10?value.toFixed(1):trafficNumber(value)}
function trafficMemory(value){return value>=1073741824?(value/1073741824).toFixed(2)+' GiB':(value/1048576).toFixed(1)+' MiB'}
function trafficTime(value){return new Date(value).toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false})}

function trafficContent(data){
 const total=data.total, count=total.requests,errors=total.errors, good=count-errors-total.client_errors;
 const rate=count?errors/count*100:0, samples=data.containers;
 const resourceOK=data.resource_status==='ok', cpu=samples.reduce((n,c)=>n+c.cpu_percent,0), memory=samples.reduce((n,c)=>n+c.memory_bytes,0);
 const resourceLabels={waiting:'Waiting for first sample',none:'No running containers',unavailable:'Container metrics unavailable',stale:'Container metrics are stale'};
 const resourceNote=resourceLabels[data.resource_status]||'Sampled '+trafficTime(data.sampled_at)+' · CPU: 100% = one core';
 const metric=(label,value,unit,note,extra='')=>'<div class="traffic-metric '+extra+'"><span>'+label+'</span><strong>'+value+'<small>'+unit+'</small></strong><p>'+note+'</p></div>';
 const latest=data.recent.slice(0,6);
 return '<div class="traffic-summary">'+
  metric('Requests',trafficNumber(count),'',count?'Through your project’s router':'Waiting for the first request')+
  metric('Server errors',trafficNumber(errors),'<span class="traffic-rate">'+rate.toFixed(1)+'%</span>','HTTP 5xx · '+trafficNumber(total.client_errors)+' client errors (4xx)',errors?'has-errors':'')+
  metric('Avg. response time',count?trafficMS(total.average_ms):'—',count?'ms':'','Router → complete response')+
  metric('Container memory',resourceOK?trafficMemory(memory).split(' ')[0]:'—',resourceOK?trafficMemory(memory).split(' ')[1]:'',resourceOK?'CPU '+cpu.toFixed(1)+'% · '+samples.length+' running':resourceNote)+
  '</div><div class="traffic-top-grid"><section class="traffic-panel"><div class="traffic-panel-head"><div><h2>Request volume</h2><span>'+data.interval_seconds+'-second intervals</span></div><div class="traffic-legend"><span><i></i>Requests</span><span><i class="errors"></i>5xx errors</span></div></div>'+trafficBars(data)+
  '<div class="traffic-distribution" aria-label="HTTP response distribution"><span style="flex:'+Math.max(good,0)+'"></span><span class="client" style="flex:'+total.client_errors+'"></span><span class="errors" style="flex:'+errors+'"></span></div><div class="traffic-status-key"><span><i></i>2xx / 3xx <strong>'+trafficNumber(good)+'</strong></span><span><i class="client"></i>4xx <strong>'+trafficNumber(total.client_errors)+'</strong></span><span><i class="errors"></i>5xx <strong>'+trafficNumber(errors)+'</strong></span></div></section>'+ 
  '<section class="traffic-panel"><div class="traffic-panel-head"><div><h2>Response time</h2><span>Average per interval</span></div><span class="traffic-unit">ms</span></div><div class="traffic-latency-number">'+(count?trafficMS(total.average_ms):'—')+'<small>'+(count?'ms':'')+'</small><span>avg / <b>'+(count?trafficMS(total.max_ms)+' ms':'—')+'</b> slowest</span></div>'+trafficLine(data,'average_ms','Response time in milliseconds','latency')+'</section></div>'+ 
  '<div class="traffic-bottom-grid"><section class="traffic-panel traffic-resources"><div class="traffic-panel-head"><div><h2>Container resources</h2><span>'+esc(resourceNote)+'</span></div>'+icon('box')+'</div><div class="traffic-resource-grid"><div><span class="traffic-kicker">CPU</span><strong>'+(resourceOK?cpu.toFixed(1)+'<small>%</small>':'—')+'</strong>'+trafficLine(data,'cpu_percent','CPU percent, 100 percent is one core','cpu')+'</div><div><span class="traffic-kicker">MEMORY</span><strong>'+(resourceOK?trafficMemory(memory):'—')+'</strong>'+trafficLine(data,'memory_bytes','Container memory in MiB','memory')+'</div></div>'+ 
  '<div class="traffic-container-list">'+(resourceOK?samples.map(c=>'<div><span>'+icon('box')+esc(c.name)+'</span><span>'+c.cpu_percent.toFixed(1)+'% CPU</span><span>'+trafficMemory(c.memory)+' / '+trafficMemory(c.limit)+'</span></div>').join(''):'<p>'+esc(resourceNote)+'. Request monitoring continues independently.</p>')+'</div></section>'+ 
  '<section class="traffic-panel traffic-recent"><div class="traffic-panel-head"><div><h2>Recent requests</h2><span>Matched route only · no query strings or client data</span></div><span class="traffic-unit">LATEST '+latest.length+'</span></div><div class="traffic-table-wrap"><table><thead><tr><th>Time</th><th>Method / route</th><th>Status</th><th>Duration</th></tr></thead><tbody>'+latest.map(r=>'<tr><td>'+trafficTime(r.at)+'</td><td><span class="traffic-method">'+esc(r.method)+'</span><span class="traffic-route" title="'+esc(r.route)+'">'+esc(r.route)+'</span></td><td><span class="traffic-http '+(r.status>=500?'server-error':r.status>=400?'client-error':'')+'">'+r.status+'</span></td><td>'+trafficMS(r.duration_ms)+'<span class="traffic-unit"> ms</span></td></tr>').join('')+(!latest.length?'<tr><td colspan="4" class="traffic-empty">Requests appear here automatically when a project route receives traffic.</td></tr>':'')+'</tbody></table></div></section></div>';
}

function trafficAxis(data){
 const buckets=data.buckets;
 return '<div class="traffic-axis"><span>'+trafficTime(buckets[0].at*1000)+'</span><span>'+trafficTime(buckets[30].at*1000)+'</span><span>Now</span></div>';
}

function trafficBars(data){
 const max=Math.max(1,...data.buckets.map(b=>b.requests));
 const bars=data.buckets.map((b,i)=>{
  const x=4+i*10,h=b.requests/max*150,eh=b.errors/max*150;
  return '<g><title>'+trafficTime(b.at*1000)+' · '+b.requests+' requests · '+b.errors+' server errors</title><rect class="traffic-bar-track" x="'+x+'" y="10" width="6" height="150" rx="2"/><rect class="traffic-bar" x="'+x+'" y="'+(160-h)+'" width="6" height="'+h+'" rx="1"/><rect class="traffic-error-bar" x="'+x+'" y="'+(160-eh)+'" width="6" height="'+eh+'" rx="1"/></g>';
 }).join('');
 return '<div class="traffic-volume-chart"><div class="traffic-y-axis"><span>'+trafficNumber(max)+'</span><span>'+trafficNumber(max/2)+'</span><span>0</span></div><svg role="img" aria-label="Requests and server errors over time, peak '+max+' requests per interval" viewBox="0 0 604 164" preserveAspectRatio="none"><path class="traffic-gridline" d="M0 10H604M0 85H604M0 160H604"/>'+bars+'</svg></div>'+trafficAxis(data);
}

function trafficLine(data,key,label,kind){
 const values=data.buckets.map(b=>key==='average_ms'?(b.requests?b[key]:null):b[key]);
 const present=values.filter(v=>v!==null),max=Math.max(kind==='memory'?1048576:1,...present);
 let path='',dots='';
 values.forEach((v,i)=>{
  if(v===null)return;
  const x=4+i*5,y=95-v/max*80;
  path+=(i===0||values[i-1]===null?'M':'L')+x.toFixed(1)+','+y.toFixed(1);
  dots+='<circle cx="'+x+'" cy="'+y+'" r="1.8"><title>'+trafficTime(data.buckets[i].at*1000)+' · '+(kind==='memory'?trafficMemory(v):v.toFixed(1)+(kind==='cpu'?'%':' ms'))+'</title></circle>';
 });
 return '<div class="traffic-line-chart '+kind+'"><svg role="img" aria-label="'+esc(label)+(present.length?'':' — no samples yet')+'" viewBox="0 0 304 108" preserveAspectRatio="none"><path class="traffic-gridline" d="M0 15H304M0 55H304M0 95H304"/><path class="traffic-line" d="'+path+'"/>'+dots+'</svg>'+(!present.length?'<span class="traffic-no-samples">'+(kind==='latency'?'No requests yet':'Waiting for samples')+'</span>':'')+'</div>'+trafficAxis(data);
}
