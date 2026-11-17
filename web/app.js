'use strict';
const $ = s => document.querySelector(s);
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const rootLabel = root => esc(!root || root==='.' ? '/ (repo root)' : root);
const icons = {
 arrow: '<path d="M7 17 17 7M7 7h10v10"/>',
 next: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
 plus: '<path d="M12 5v14M5 12h14"/>',
 grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
 layers: '<path d="m12 3 10 5-10 5L2 8l10-5Zm-9 9 9 5 9-5M3 16l9 5 9-5"/>',
 box: '<path d="m12 3 9 5v9l-9 5-9-5V8l9-5Zm0 10v9M3 8l9 5 9-5M7.5 5.5l9 5"/>',
 server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M16 6.5h2M16 17.5h2"/>',
 branch: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="5" r="2"/><path d="M6 7v10M18 7a8 8 0 0 1-8 8H6"/>',
 folder: '<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
 globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a20 20 0 0 1 0 18 20 20 0 0 1 0-18Z"/>',
 activity: '<path d="M2 12h5l3-8 4 16 3-8h5"/>',
 terminal: '<path d="m7 8 4 4-4 4m7 0h4"/><rect x="2" y="3" width="20" height="18" rx="3"/>',
 copy: '<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
 expand: '<path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/>',
 settings: '<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3" fill="var(--bg)"/><circle cx="16" cy="17" r="3" fill="var(--bg)"/>',
 search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>',
 lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>',
 logout: '<path d="M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4m6-13 5 5-5 5m-6-5h11"/>',
 menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
 sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',
 moon: '<path d="M20.7 13A9 9 0 0 1 11 3.3 9 9 0 1 0 20.7 13Z"/>',
 chevron: '<path d="m8 9 4-4 4 4m-8 6 4 4 4-4"/>',
 back: '<path d="M19 12H5m6-6-6 6 6 6"/>',
 close: '<path d="m6 6 12 12M6 18 18 6"/>'
};
function icon(name){return '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'+(icons[name]||icons.box)+'</svg>'}
function brand(){return '<span class="mark">'+icon('arrow')+'</span><span>Startship</span>'}
let user, workspaces=[], workspace, timer, selectedDeployment='', viewVersion=0, accountSaving=false;
function themeButton(){
 const light=document.documentElement.dataset.theme==='light';
 return '<button class="theme-toggle" id="theme-toggle" aria-label="Switch to '+(light?'dark':'light')+' theme" title="Switch to '+(light?'dark':'light')+' theme">'+icon(light?'moon':'sun')+'<span>'+(light?'Dark':'Light')+'</span></button>';
}
function bindTheme(){
 $('#theme-toggle').onclick=()=>{
  const theme=document.documentElement.dataset.theme==='light'?'dark':'light';
  document.documentElement.dataset.theme=theme;
  try{localStorage.setItem('startship-theme',theme)}catch{}
  $('#theme-toggle').outerHTML=themeButton();bindTheme();$('#theme-toggle').focus();
 };
}
async function api(path,body,method){const requestUser=user,r=await fetch('/api/'+path,{method:method||(body?'POST':'GET'),headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});const v=await r.json();if(!r.ok){if(r.status===401&&user&&user===requestUser&&!accountSaving){user=null;location.hash='login'}throw Error(v.error||'Request failed')}return v}
function toast(text){$('#toast').textContent=text;$('#toast').style.display='block';setTimeout(()=>$('#toast').style.display='none',3500)}
function badge(status){return '<span class="badge '+esc(status||'idle')+'">'+esc(status||'Not deployed')+'</span>'}
function empty(title,text){return '<div class="panel empty"><span class="card-icon">'+icon('box')+'</span><strong>'+esc(title)+'</strong>'+esc(text)+'</div>'}
function field(label,name,value='',type='text',extra=''){return '<label>'+esc(label)+'<input name="'+name+'" type="'+type+'" value="'+esc(value)+'" '+extra+'></label>'}
function nav(hash){if(location.hash==='#'+hash)render();else location.hash=hash}
function modal(title,description,fields,submit,onSubmit){
 const d=$('#modal');d.setAttribute('aria-labelledby','modal-title');d.innerHTML='<div class="row"><h2 id="modal-title">'+esc(title)+'</h2><button class="icon-button" type="button" id="close-modal" aria-label="Close">'+icon('close')+'</button></div><p class="small">'+esc(description)+'</p><form><div class="modal-body">'+fields+'<div class="form-error" role="alert"></div></div><div class="form-actions"><button type="button" id="cancel-modal">Cancel</button><button class="primary" type="submit">'+esc(submit)+'</button></div></form>';
 $('#close-modal').onclick=$('#cancel-modal').onclick=()=>d.close();
 d.querySelector('form').onsubmit=async e=>{e.preventDefault();const button=d.querySelector('[type=submit]');button.disabled=true;d.querySelector('.form-error').textContent='';try{if(await onSubmit(new FormData(e.target),e.target)!==false)d.close()}catch(err){d.querySelector('.form-error').textContent=err.message}finally{button.disabled=false}};d.showModal();d.querySelector('form input,form select,form textarea')?.focus();
}
function shell(content,project=null){
 const workspaceURL='#workspace/'+esc(workspace?.id||''),settings=location.hash.endsWith('/settings')&&!project;
 const projectSection=location.hash.split('/')[2]||'deployments';
 const sidebarLink=(href,shape,label,active)=>'<a href="'+href+'" '+(active?'aria-current="page"':'')+'>'+icon(shape)+'<span>'+esc(label)+'</span></a>';
 const sidebar='<aside class="sidebar" aria-label="Dashboard sidebar"><div class="sidebar-top"><a href="'+workspaceURL+'" class="brand">'+brand()+'</a><button id="close-navigation" class="icon-button mobile-only" aria-label="Close navigation">'+icon('close')+'</button></div>'+
  '<button class="sidebar-selector" id="workspace-switch" popovertarget="workspace-menu" aria-expanded="false"><span class="workspace-mark">'+icon('layers')+'</span><span class="selector-copy"><small>Workspace</small><strong>'+esc(workspace?.name||'Choose workspace')+'</strong></span>'+icon('chevron')+'</button>'+
  '<div id="workspace-menu" class="sidebar-menu" popover><div class="menu-caption">Switch workspace</div><div class="workspace-options">'+workspaces.map(w=>'<a href="#workspace/'+esc(w.id)+'" '+(workspace?.id===w.id?'aria-current="true"':'')+'><span class="workspace-initial">'+esc(w.name[0]?.toUpperCase())+'</span><span>'+esc(w.name)+'</span>'+(workspace?.id===w.id?'<span class="current-dot" aria-label="Current workspace"></span>':'')+'</a>').join('')+'</div><div class="menu-divider"></div><button id="create-workspace">'+icon('plus')+'Create workspace</button><a href="#workspaces">'+icon('layers')+'Manage workspaces</a></div>'+
  '<div class="sidebar-body"><nav class="sidebar-nav" aria-label="'+(project?'Project':'Workspace')+' navigation">'+(project?
   sidebarLink(workspaceURL,'back','All projects',false)+'<div class="sidebar-project"><strong>'+esc(project.name)+'</strong><a class="sidebar-source" href="#project/'+esc(project.id)+'/source" title="Project source">'+icon('branch')+'<span>'+esc(project.git_url?(project.branch||'main'):'Connect source')+'</span></a></div>'+projectSections.map(([key,label,shape])=>sidebarLink('#project/'+esc(project.id)+'/'+key,shape,label,key===projectSection)).join(''):
   '<div class="sidebar-caption">Workspace</div>'+sidebarLink(workspaceURL,'grid','Projects',!settings&&location.hash!=='#workspaces')+sidebarLink(workspaceURL+'/settings','settings','Workspace settings',settings))+'</nav>'+(project?'<section class="sidebar-deployment" aria-label="Project deployment"><div class="deployment-status"><span>Latest release</span><span id="project-state" aria-label="Latest deployment">'+badge(project.deployment_status)+'</span></div><p id="project-summary" aria-live="polite">'+esc(project.summary)+'</p><button class="primary" id="deploy-project">'+icon('plus')+'Deploy project</button></section>':'')+'</div>'+
  '<div class="sidebar-bottom"><span class="sidebar-instance"><span class="dot"></span>Self-hosted instance</span><button class="sidebar-selector account-selector" id="account-switch" popovertarget="account-menu" aria-expanded="false"><span class="avatar">'+esc(user.username[0].toUpperCase())+'</span><span class="selector-copy"><strong>'+esc(user.username)+'</strong><small>Personal account</small></span>'+icon('chevron')+'</button></div>'+
  '<div id="account-menu" class="sidebar-menu" popover><div class="menu-caption">Signed in as <strong>'+esc(user.username)+'</strong></div><button id="account-profile">'+icon('settings')+'Account settings</button>'+themeButton()+'<div class="menu-divider"></div><button id="logout">'+icon('logout')+'Sign out</button></div></aside>';
 $('#root').innerHTML='<div class="dashboard sidebar-layout"><div id="desktop-sidebar">'+sidebar+'</div><dialog id="navigation-drawer" aria-label="Navigation"></dialog><div class="dashboard-body"><main class="main"><div class="content">'+content+'</div></main><footer class="app-footer"><span>Startship <span class="footer-separator">/</span> Deployment platform</span><span>Your infrastructure. No middleman.</span></footer></div></div>';
 bindTheme();bindSidebar();
}
function focusMenuItem(menu,item){
 if(!item)return;
 item.focus({preventScroll:true});
 for(let parent=item.parentElement;parent;parent=parent.parentElement){
  if(parent.scrollHeight>parent.clientHeight){
   const frame=parent.getBoundingClientRect(),rect=item.getBoundingClientRect(),top=frame.top+parent.clientTop,bottom=top+parent.clientHeight;
   if(rect.top<top)parent.scrollTop-=top-rect.top;
   else if(rect.bottom>bottom)parent.scrollTop+=rect.bottom-bottom;
  }
  if(parent===menu)break;
 }
}
function bindSidebar(){
 const drawer=$('#navigation-drawer'),sidebar=$('.sidebar'),desktop=$('#desktop-sidebar');
 const closeDrawer=()=>{drawer.close();desktop.append(sidebar)};
 $('#open-navigation').onclick=()=>{drawer.append(sidebar);drawer.showModal();$('#close-navigation').focus()};
 $('#close-navigation').onclick=closeDrawer;
 drawer.onclose=()=>desktop.append(sidebar);
 drawer.onclick=e=>{if(e.target===drawer)closeDrawer()};
 sidebar.querySelectorAll('a').forEach(a=>a.addEventListener('click',()=>{sidebar.querySelectorAll('[popover]').forEach(p=>{if(p.matches(':popover-open'))p.hidePopover()});if(drawer.open)closeDrawer()}));
 sidebar.querySelectorAll('[popovertarget]').forEach(button=>{
  const menu=$('#'+button.getAttribute('popovertarget'));
  menu.addEventListener('toggle',()=>{
   const open=menu.matches(':popover-open');button.setAttribute('aria-expanded',String(open));if(!open)return;
   menu.style.maxHeight='';
   const rect=button.getBoundingClientRect(),height=menu.offsetHeight,below=innerHeight-rect.bottom-12,above=rect.top-12;
   menu.style.left=Math.max(8,Math.min(rect.left,innerWidth-menu.offsetWidth-8))+'px';
   menu.style.top=(below>=height||below>=above?rect.bottom+6:Math.max(8,rect.top-height-6))+'px';
   menu.style.maxHeight=Math.max(80,(below>=height||below>=above?below:above))+'px';
  });
  button.onkeydown=e=>{if(e.key==='ArrowDown'){e.preventDefault();menu.showPopover();focusMenuItem(menu,menu.querySelector('a,button'))}};
  menu.onkeydown=e=>{if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key))return;e.preventDefault();const items=[...menu.querySelectorAll('a,button')],index=items.indexOf(document.activeElement);focusMenuItem(menu,items[e.key==='Home'?0:e.key==='End'?items.length-1:(index+(e.key==='ArrowDown'?1:items.length-1))%items.length])};
 });
 $('#create-workspace').onclick=()=>{$('#workspace-menu').hidePopover();if(drawer.open)closeDrawer();createWorkspace()};
 $('#account-profile').onclick=()=>{
  $('#account-menu').hidePopover();if(drawer.open)closeDrawer();
  accountSettings();
 };
 $('#logout').onclick=async()=>{try{await api('logout',{});if(drawer.open)closeDrawer();user=null;nav('login')}catch(err){toast(err.message)}};
}
function accountSettings(){
 modal('Account settings','Update your username or password. Confirm your current password to save changes.',
  field('Username','username',user.username,'text','required maxlength="100" autocomplete="username"')+
  '<div class="split">'+field('New password (optional)','new_password','','password','minlength="8" maxlength="72" autocomplete="new-password"')+field('Confirm new password','confirm_password','','password','maxlength="72" autocomplete="new-password"')+'</div>'+
  field('Current password','current_password','','password','required autocomplete="current-password"')+
  '<div class="note">New passwords must be 8–72 bytes. Saving signs out your other sessions; this session stays signed in.</div>',
  'Save account',async(f,form)=>{
   const password=f.get('new_password');if(password!==f.get('confirm_password'))throw Error('New passwords do not match');
   const changes={username:f.get('username'),current_password:f.get('current_password')};if(password)changes.new_password=password;
   accountSaving=true;try{user=await api('me',changes,'PATCH');form.reset();await render();toast('Account updated')}finally{accountSaving=false}
  });
}
function heading(kicker,title,subtitle,actions='',subtitleId='',mobileContext=''){return '<header class="heading"><div class="heading-identity"><button class="icon-button mobile-only" id="open-navigation" aria-label="Open navigation" aria-haspopup="dialog">'+icon('menu')+'</button><div class="heading-copy">'+(kicker?'<div class="eyebrow">'+kicker+'</div>':'')+(mobileContext?'<span class="mobile-project-name">'+esc(mobileContext)+'</span>':'')+'<h1>'+esc(title)+'</h1>'+(subtitle?'<p'+(subtitleId?' id="'+esc(subtitleId)+'" aria-live="polite"':'')+'>'+esc(subtitle)+'</p>':'')+'</div></div>'+(actions?'<div class="row heading-actions">'+actions+'</div>':'')+'</header>'}
function auth(setup){
 $('#root').innerHTML='<div class="auth"><header class="auth-header"><a class="brand" href="#login">'+brand()+'</a>'+themeButton()+'</header><section class="auth-story"><div class="auth-index"><span>01 — YOUR INFRASTRUCTURE</span><span>BUILT TO SHIP ↗</span></div><div class="auth-message"><h1>From commit.<br>To production.<br><em>On your terms.</em></h1><p>Deploy your applications on the server you own.<br>A straight line from your code to the world.</p></div><div class="pipeline"><div class="pipeline-head"><span>THE SHORTEST PATH TO LIVE</span>'+icon('arrow')+'</div><div class="pipeline-track"><div><span class="pipeline-node">'+icon('branch')+'</span><strong>Push</strong><span>Your repository</span></div><div><span class="pipeline-node">'+icon('box')+'</span><strong>Build</strong><span>Isolated by default</span></div><div><span class="pipeline-node node-live">'+icon('globe')+'</span><strong>Ship</strong><span>Your own domain</span></div></div><div class="pipeline-command"><span>$</span> git push origin main <span class="command-cursor"></span></div></div></section><section class="auth-form"><form><span class="form-index">'+(setup?'FIRST THINGS FIRST':'YOUR CONTROL ROOM')+'</span><h2>'+(setup?'Set up your account.':'Welcome back.')+'</h2><p>'+(setup?'Create the administrator account for this server.':'Sign in to manage your applications.')+'</p>'+field('Username','username','','text','required autocomplete="username" placeholder="Enter your username"')+field('Password','password','','password','required minlength="8" autocomplete="'+(setup?'new-password':'current-password')+'" placeholder="Enter your password"')+(setup?'<p class="password-help">At least 8 characters. There is no password reset, so keep it somewhere safe.</p>':'')+'<div class="form-error" role="alert"></div><button class="primary" type="submit">'+(setup?'Create account':'Sign in')+icon('arrow')+'</button><div class="form-footnote">'+icon('lock')+'Private access to your infrastructure.</div></form></section><footer class="auth-footer"><span>SELF-HOSTED, NOT LIMITED.</span><span>Startship / Deployment platform</span></footer></div>';
 bindTheme();
 $('form').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('button');button.disabled=true;const v=Object.fromEntries(new FormData(e.target));try{if(setup)await api('setup',v);await api('login',v);user=await api('me');nav('workspaces')}catch(err){$('.form-error').textContent=err.message}finally{button.disabled=false}};
}
function createWorkspace(){modal('Create workspace','A home for your projects.',field('Workspace name','name','','text','required'),'Create workspace',async f=>{const w=await api('workspaces',Object.fromEntries(f));nav('workspace/'+w.id);toast('Workspace created')})}
function sourceFields(project={}){
 return field('Project repository URL','git_url',project.git_url||'','url','required placeholder="https://github.com/you/project"')+field('Shared branch','branch',project.branch||'main','text','required')+field('Repository token (leave blank to keep existing)','git_token','','password','autocomplete="off"');
}
function sourceData(f){const v={git_url:f.get('git_url'),branch:f.get('branch')};if(f.get('git_token'))v.git_token=f.get('git_token');return v}
function editProjectSource(project){
 modal('Project source','One repository, branch, and credential set for this project. Changes apply to the whole project on its next deployment.',(project.source_conflict?'<div class="note">Legacy containers used different sources. Choose the single repository for this project. Running containers stay online until the next healthy deployment.</div>':'')+sourceFields(project),'Save project source',async f=>{await api('projects/'+project.id,sourceData(f),'PATCH');await render();toast('Project source saved')});
}
function createProject(){
 modal('Create project','One repository, one deployment. Startship detects the containers your project needs.',field('Project name','name','','text','required placeholder="My Website"')+sourceFields(),'Create project',async f=>{
  const p=await api('projects',{name:f.get('name'),workspace_id:workspace.id,...sourceData(f)});nav('project/'+p.id);toast('Project created');
 });
}
const types=['auto','dockerfile','static','node','python','go','rust'];
const projectSections=[['deployments','Deployments','activity'],['logs','Logs','terminal'],['containers','Containers','box'],['domains','Domains','globe'],['source','Source','branch'],['environment','Environment','lock'],['settings','Settings','settings']];
const busyRelease=d=>d&&['queued','building','testing','starting'].includes(d.status);
function environmentData(value){
 const env={};for(const line of value.split('\n')){if(!line.trim()||line.trim().startsWith('#'))continue;const i=line.indexOf('=');if(i<1||!/^[A-Za-z_][A-Za-z0-9_]*$/.test(line.slice(0,i).trim()))throw Error('Use KEY=VALUE, one variable per line.');env[line.slice(0,i).trim()]=line.slice(i+1)}return env;
}
function projectSummary(containers,routes){return containers.length+' container'+(containers.length===1?'':'s')+' · '+containers.filter(c=>c.status==='running').length+' running · '+routes.length+' domain route'+(routes.length===1?'':'s')}
function addRoute(project,containers,route={}){
 if(!containers.length){toast('Deploy the project to detect its containers first');return}
 modal(route.id?'Edit domain':'Connect domain','Hostnames and path prefixes route to the project. Longer prefixes take priority.',field('Hostname','host',route.host||'','text','required placeholder="example.com"')+field('Path prefix','path_prefix',route.path_prefix||'/','text','required')+(containers.length>1?'<label>Target container<select name="container_id">'+containers.map(c=>'<option value="'+esc(c.id)+'" '+((route.container_id||route.app_id)===c.id?'selected':'')+'>'+esc(c.name)+' · :'+c.port+'</option>').join('')+'</select></label>':'<input type="hidden" name="container_id" value="'+esc(containers[0].id)+'"><p class="small muted">Target: '+esc(containers[0].name)+'</p>')+'<label class="inline-check"><input type="checkbox" name="strip_prefix" '+(route.strip_prefix?'checked':'')+'>Strip path prefix before forwarding</label><p class="note">Point this hostname’s DNS A record to your server’s public IP.</p>',route.id?'Save domain':'Connect domain',async f=>{await api('routes'+(route.id?'/'+route.id:''),{...Object.fromEntries(f),project_id:project.id,strip_prefix:f.has('strip_prefix')},route.id?'PATCH':'POST');await render();toast('Domain saved')});
}
function sourceContent(project){
 const webhook=project.webhook_secret?location.origin+'/api/webhooks/github/'+project.id:'';
 return '<div class="integration-layout"><section class="panel panel-pad"><div class="section-head"><h2>Project source</h2><button id="edit-project-source">Edit source</button></div><p class="small muted">A single repository and branch for every container.</p><dl class="source-details"><dt>Repository</dt><dd>'+esc(project.git_url||'Not connected')+'</dd><dt>Branch</dt><dd>'+esc(project.branch||'main')+'</dd><dt>Credentials</dt><dd>'+(project.has_git_token?'Token configured':'No token')+'</dd></dl></section><section class="panel panel-pad"><h2>GitHub webhook</h2><p class="small muted">A signed push queues one project deployment when automatic deployment is enabled in Settings.</p>'+field('Payload URL','webhook_url',webhook,'text','readonly')+field('Webhook secret','webhook_secret',project.webhook_secret||'Connect a source first','password','readonly')+'<button id="reveal-webhook" type="button">Show secret</button><p class="small muted">Content type: application/json · Event: push</p></section></div>';
}
function projectSettings(project){
 return '<form id="project-settings" class="panel panel-pad project-form"><div class="section-head"><h2>Project settings</h2><span class="small muted">Shared by every container</span></div><div class="split">'+field('Project name','name',project.name,'text','required')+field('Repository root','root_dir',project.root_dir||'.','text','required')+'</div><div class="split"><label>Build strategy<select name="build_type">'+types.map(t=>'<option '+((project.build_type||'auto')===t?'selected':'')+'>'+t+'</option>').join('')+'</select></label>'+field('HTTP port override (0 = detect)','port',project.port||0,'number','min="0" max="65535" required')+'</div>'+field('Test command (optional, run in each detected container)','test_command',project.test_command||'')+'<label class="inline-check"><input type="checkbox" name="auto_deploy" '+(project.auto_deploy?'checked':'')+'>Deploy project on source pushes</label><p class="note">Auto detects one server at the repository root, or multiple server folders in a monorepo. A manual build strategy treats the selected root as one container. Changes apply together on the next deployment.</p><div class="form-error" role="alert"></div><button class="primary">Save project settings</button></form>';
}
function environmentContent(project){
 return '<form id="project-environment" class="panel panel-pad project-form"><div class="section-head"><h2>Environment</h2><span class="small muted">Project-wide configuration</span></div><p class="small muted">Applied to every container on the next deployment. PORT is assigned automatically unless overridden.</p><label>Variables<textarea name="env" rows="14" spellcheck="false" placeholder="NODE_ENV=production">'+esc(Object.entries(project.env||{}).map(([k,v])=>k+'='+v).join('\n'))+'</textarea></label><div class="form-error" role="alert"></div><button class="primary">Save environment</button></form>';
}
function releaseEntries(releases,selected){
 return releases.map(d=>'<button class="release-entry select-deploy '+(d.id===selected?'selected':'')+'" data-release="'+esc(d.id)+'" aria-pressed="'+(d.id===selected)+'"><span class="release-main"><span class="release-id">'+icon('branch')+esc(d.commit?.slice(0,7)||d.id.slice(0,7))+'</span>'+badge(d.status)+'</span><span class="release-meta"><time>'+esc(new Date(d.created).toLocaleString())+'</time><span>'+((d.container_releases||[]).length)+' containers'+(d.redeploy_of?' · redeployed':'')+'</span></span></button>').join('')||'<div class="panel-pad muted small">No project deployments yet.</div>';
}
function logPanel(runtime=false){
 return '<section class="panel log-panel project-log-panel"><div class="log-head"><div class="log-title">'+icon('terminal')+'<strong id="log-heading">'+(runtime?'Live logs':'Build output')+'</strong><span id="log-state"></span></div><div class="log-actions"><label class="inline-check"><input id="follow-log" type="checkbox" checked>Follow</label><button class="icon-button" id="copy-log" aria-label="Copy logs">'+icon('copy')+'</button></div></div><pre class="log" id="project-log" aria-label="Project logs">Select a release or deploy your project.</pre></section>';
}
function containerEntries(project,containers){
 return containers.map(c=>'<div class="container-row"><span class="card-icon">'+icon('box')+'</span><div class="container-copy"><strong>'+esc(c.name)+'</strong><span>'+rootLabel(c.root_dir)+' · '+esc(c.build_type)+' · :'+esc(c.port)+'</span></div>'+badge(c.status)+'<a class="button" href="#project/'+esc(project.id)+'/logs/'+esc(c.id)+'">'+icon('terminal')+'Logs</a></div>').join('')||'<p class="panel-pad small muted">No containers yet. Deploy the project; Startship detects and launches them automatically.</p>';
}
function domainEntries(project,containers,routes){
 return routes.map(r=>'<div class="container-row"><span class="card-icon">'+icon('globe')+'</span><div class="container-copy"><a href="'+esc(r.url||'')+'" target="_blank" rel="noopener noreferrer">'+esc(r.host)+esc(r.path_prefix)+'</a><span>'+esc(containers.find(c=>c.id===(r.container_id||r.app_id))?.name||'Container unavailable')+(r.strip_prefix?' · strips prefix':'')+'</span></div><button data-edit-route="'+esc(r.id)+'" aria-label="Edit '+esc(r.host)+'">Edit</button><button data-delete-route="'+esc(r.id)+'" aria-label="Delete '+esc(r.host)+'">'+icon('close')+'</button></div>').join('')||'<p class="panel-pad small muted">No domains connected. Deploy the project, then point a hostname to it.</p>';
}
async function projectPage(id,section='deployments',runtimeID=''){
 const version=viewVersion;
 if(['git','webhook'].includes(section))section='source';
 if(!projectSections.some(([key])=>key===section))section='deployments';
 let [project,containers,releases,routes]=await Promise.all([api('projects/'+id),api('containers?project_id='+id),api('releases?project_id='+id+'&logs=false'),api('routes?project_id='+id)]);
 if(version!==viewVersion)return;
 workspace=workspaces.find(w=>w.id===project.workspace_id)||workspace;if(workspace)localStorage.setItem('workspace',workspace.id);
 const sort=()=>releases.sort((a,b)=>new Date(b.created)-new Date(a.created));sort();
 if(project.active_deployment)containers=containers.filter(c=>c.active_deployment||(busyRelease(releases[0])&&c.release_id===releases[0].id));
 let selected=releases[0]?.id||'',followLatest=true,logVersion=0;
 const title=projectSections.find(([key])=>key===section)[1];
 const warning=(!releases.length&&containers.some(c=>c.active_deployment)?'<p class="note">Existing containers remain online. Deploy the project to start its unified release history.</p>':'')+(project.config_conflict?'<div class="note">Legacy containers used different settings. Review <a href="#project/'+esc(id)+'/environment">Environment</a> and <a href="#project/'+esc(id)+'/settings">Settings</a>, then confirm one shared configuration. Existing deployments stay online.<button id="confirm-project-config">Confirm shared configuration</button></div>':'')+(project.source_conflict?'<div class="note">Choose one <a href="#project/'+esc(id)+'/source">project source</a> before deploying.</div>':'');
 let content='';
 if(section==='deployments')content='<div class="project-deployments"><section class="release-history"><div class="section-head"><h2>Project releases</h2><span class="count" id="release-count">'+releases.length+'</span></div><div class="panel release-directory" id="project-releases">'+releaseEntries(releases,selected)+'</div><div class="release-toolbar"><label class="inline-check"><input id="no-cache" type="checkbox">Build without cache</label><button id="redeploy-release">Redeploy selected</button></div></section>'+logPanel()+'</div>';
 if(section==='logs')content='<div class="section-head"><h2>Logs</h2><label class="runtime-selector">Log source<select id="log-source"><option value="">Project build output</option>'+containers.map(c=>'<option value="'+esc(c.id)+'" '+(runtimeID===c.id?'selected':'')+'>'+esc(c.name)+' · runtime</option>').join('')+'</select></label></div>'+logPanel(true);
 if(section==='containers')content='<div class="section-head"><h2>Detected containers</h2><span class="small muted">Managed together by the project</span></div><div class="panel" id="project-containers">'+containerEntries(project,containers)+'</div>';
 if(section==='domains')content='<div class="section-head"><h2>Domains</h2><button class="primary" id="add-project-domain">'+icon('plus')+'Connect domain</button></div><div class="panel">'+domainEntries(project,containers,routes)+'</div><p class="note">One container is the default target. With multiple containers, choose a target for each hostname or path. Longer matching path prefixes take priority.</p>';
 if(section==='source')content=sourceContent(project);
 if(section==='environment')content=environmentContent(project);
 if(section==='settings')content=projectSettings(project);
 shell(heading('',title,'','<button class="primary mobile-only" id="deploy-project-mobile">'+icon('plus')+'Deploy</button>','',project.name)+warning+content,{...project,summary:projectSummary(containers,routes),deployment_status:releases[0]?.status});
 const bindForm=(selector,data)=>{const form=$(selector);if(!form)return;form.onsubmit=async e=>{e.preventDefault();const button=form.querySelector('button');button.disabled=true;try{await api('projects/'+id,data(new FormData(form)),'PATCH');await render();toast('Project saved; changes apply on next deployment')}catch(err){form.querySelector('.form-error').textContent=err.message}finally{button.disabled=false}}};
 if(section==='settings')bindForm('#project-settings',f=>({name:f.get('name'),root_dir:f.get('root_dir'),build_type:f.get('build_type'),port:Number(f.get('port')),test_command:f.get('test_command'),auto_deploy:f.has('auto_deploy')}));
 if(section==='environment')bindForm('#project-environment',f=>({env:environmentData(f.get('env'))}));
 if(section==='source'){$('#edit-project-source').onclick=()=>editProjectSource(project);$('#reveal-webhook').onclick=()=>{const input=$('[name=webhook_secret]');input.type=input.type==='password'?'text':'password';$('#reveal-webhook').textContent=input.type==='password'?'Show secret':'Hide secret'};}
 if(project.config_conflict)$('#confirm-project-config').onclick=async()=>{try{await api('projects/'+id,{config_confirmed:true},'PATCH');await render();toast('Project configuration confirmed')}catch(e){toast(e.message)}};
 if(section==='domains'){
  $('#add-project-domain').onclick=()=>addRoute(project,containers);
  document.querySelectorAll('[data-edit-route]').forEach(b=>b.onclick=()=>addRoute(project,containers,routes.find(r=>r.id===b.dataset.editRoute)));
  document.querySelectorAll('[data-delete-route]').forEach(b=>b.onclick=async()=>{if(!confirm('Disconnect this domain route?'))return;try{await api('routes/'+b.dataset.deleteRoute,null,'DELETE');await render()}catch(e){toast(e.message)}});
 }
 const loadLog=async()=>{
  if(!['logs','deployments'].includes(section))return;
  const request=++logVersion,source=section==='logs'?$('#log-source').value:'';
  try{
   const d=source?await api('containers/'+source+'/logs'):(selected?await api('releases/'+selected):{log:'No project deployments yet. Deploy project to detect and launch its containers.'});
   if(version!==viewVersion||request!==logVersion)return;
   const pre=$('#project-log');pre.textContent=d.log||'Waiting for output…';if($('#follow-log').checked)pre.scrollTop=pre.scrollHeight;
   $('#log-heading').textContent=source?(containers.find(c=>c.id===source)?.name||'Container')+' · runtime':'Project build output';
   $('#log-state').innerHTML=badge(d.status||(source?'live':'idle'));
   if(section==='deployments')$('#redeploy-release').disabled=!d.redeployable||!!busyRelease(releases[0]);
  }catch(e){if(version===viewVersion&&request===logVersion)$('#project-log').textContent=e.message}
 };
 const renderReleases=()=>{
  const panel=$('#project-releases'),markup=releaseEntries(releases,selected);if(panel.innerHTML===markup)return;
  const focused=document.activeElement?.dataset.release,scroll=panel.scrollTop;
  panel.innerHTML=markup;panel.scrollTop=scroll;bindReleases();
  if(focused)[...document.querySelectorAll('[data-release]')].find(b=>b.dataset.release===focused)?.focus({preventScroll:true});
 };
 const bindReleases=()=>document.querySelectorAll('[data-release]').forEach(b=>b.onclick=async()=>{followLatest=false;selected=b.dataset.release;renderReleases();await loadLog()});
 if(section==='deployments')bindReleases();
 if(section==='logs')$('#log-source').onchange=loadLog;
 if(['logs','deployments'].includes(section))$('#copy-log').onclick=async()=>{try{await navigator.clipboard.writeText($('#project-log').textContent);toast('Logs copied')}catch{toast('Clipboard unavailable')}};
 const setDeployDisabled=disabled=>{$('#deploy-project').disabled=$('#deploy-project-mobile').disabled=disabled};
 const refresh=async()=>{
  try{
   const data=await Promise.all([api('projects/'+id),api('containers?project_id='+id),api('releases?project_id='+id+'&logs=false')]);if(version!==viewVersion)return;
   [project,containers,releases]=data;sort();
   if(project.active_deployment)containers=containers.filter(c=>c.active_deployment||(busyRelease(releases[0])&&c.release_id===releases[0].id));
   if(followLatest)selected=releases[0]?.id||'';
   $('#project-summary').textContent=projectSummary(containers,routes);$('#project-state').innerHTML=badge(releases[0]?.status);setDeployDisabled(!!busyRelease(releases[0])||!!project.config_conflict||!!project.source_conflict||!project.git_url);
   if(section==='deployments'){renderReleases();$('#release-count').textContent=releases.length}
   if(section==='containers')$('#project-containers').innerHTML=containerEntries(project,containers);
   if(section==='logs'){const source=$('#log-source').value;$('#log-source').innerHTML='<option value="">Project build output</option>'+containers.map(c=>'<option value="'+esc(c.id)+'" '+(source===c.id?'selected':'')+'>'+esc(c.name)+' · runtime</option>').join('')}
   await loadLog();
  }catch(e){if(version===viewVersion)toast(e.message)}
 };
 const deployProject=async()=>{setDeployDisabled(true);try{const d=await api('projects/'+id+'/deploy',{no_cache:section==='deployments'&&$('#no-cache').checked});selected=d.id;followLatest=true;await refresh();toast('Project deployment queued')}catch(e){toast(e.message);setDeployDisabled(false)}};
 $('#deploy-project').onclick=$('#deploy-project-mobile').onclick=deployProject;
 if(section==='deployments')$('#redeploy-release').onclick=async()=>{try{const d=await api('projects/'+id+'/redeploy',{deployment_id:selected});selected=d.id;followLatest=true;await refresh();toast('Project release queued without rebuilding')}catch(e){toast(e.message)}};
 setDeployDisabled(!!busyRelease(releases[0])||!!project.config_conflict||!!project.source_conflict||!project.git_url);
 await loadLog();if(version===viewVersion)timer=setInterval(refresh,2000);
}
async function appPage(id,tab='deployments'){const version=viewVersion,container=await api('containers/'+id);if(version===viewVersion)nav('project/'+container.project_id+'/'+(tab==='settings'?'settings':tab==='domains'?'domains':'logs'))}
function collectionCard(item,kind,containers=[],releases=[]){
 const current=kind==='workspace'&&workspace?.id===item.id,owned=containers.filter(c=>c.project_id===item.id&&c.active_deployment);
 if(kind==='project'){
  const latest=releases.filter(d=>d.project_id===item.id).sort((a,b)=>new Date(b.created)-new Date(a.created))[0];
  return '<a class="project-row" data-project-name="'+esc(item.name.toLowerCase())+'" href="#project/'+esc(item.id)+'"><span class="card-icon">'+icon('grid')+'</span><span class="project-row-copy"><h3>'+esc(item.name)+'</h3><span>'+owned.length+' containers · '+esc(item.git_url?item.branch||'main':'Connect a repository')+'</span></span>'+badge(latest?.status||(owned.some(c=>c.status==='running')?'running':''))+icon('arrow')+'</a>';
 }
 const created=item.created?new Date(item.created).toLocaleDateString(undefined,{month:'short',day:'numeric'}):'';
 return '<a class="card link" href="#workspace/'+esc(item.id)+'"><div class="card-top"><div class="card-icon">'+icon('layers')+'</div>'+(current?'<span class="badge running">Current</span>':icon('arrow'))+'</div><h3>'+esc(item.name)+'</h3><div class="card-footer"><span>'+(created?'Created '+esc(created):'Workspace')+'</span><span>Open workspace '+icon('arrow')+'</span></div></a>';
}
function releasePanel(releases,projects){
 const latest=releases.slice().sort((a,b)=>new Date(b.created)-new Date(a.created)),recent=latest.slice(0,32).reverse();
 return '<aside class="panel activity-panel"><div class="section-head"><h2>Deployment activity</h2>'+icon('activity')+'</div><div class="activity-total"><strong>'+releases.length+'</strong><span>project releases</span></div>'+
 (recent.length?'<div class="release-pulse" aria-label="Recent deployment statuses">'+recent.map(d=>'<span class="'+esc(d.status)+'" title="'+esc(d.status)+'"></span>').join('')+'</div>':'<p class="activity-empty">Your first release starts with a deploy.</p>')+
 '<div class="release-list"><h2>Recent releases</h2>'+latest.slice(0,4).map(d=>'<a class="release-item" href="#project/'+esc(d.project_id)+'"><div><strong>'+esc(projects.find(p=>p.id===d.project_id)?.name||'Project')+'</strong><small>'+esc(d.commit?.slice(0,7)||d.id.slice(0,7))+'</small></div>'+badge(d.status)+'</a>').join('')+'</div></aside>';
}
function workspaceSettings(){
 shell(heading('Workspace','Workspace settings','A shared home for your projects.')+
  '<form id="workspace-settings" class="panel panel-pad workspace-settings">'+field('Workspace name','name',workspace.name,'text','required')+'<div class="form-error" role="alert"></div><button class="primary" type="submit">Save changes</button></form>');
 $('#workspace-settings').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('button');button.disabled=true;try{await api('workspaces/'+workspace.id,Object.fromEntries(new FormData(e.target)),'PATCH');await render();toast('Workspace saved')}catch(err){if($('#workspace-settings'))$('#workspace-settings .form-error').textContent=err.message}finally{button.disabled=false}};
}
async function render(){
 clearInterval(timer);const version=++viewVersion;
 try{
 const setup=await api('setup');if(version!==viewVersion)return;if(setup.required){auth(true);return}
 if(!user){try{user=await api('me')}catch{auth(false);return}}
 const parts=location.hash.slice(1).split('/');if(parts[0]==='login'){auth(false);return}
 workspaces=await api('workspaces');if(version!==viewVersion)return;workspace=workspaces.find(w=>w.id===parts[1])||workspaces.find(w=>w.id===localStorage.getItem('workspace'))||workspaces[0];
 if(!parts[0]&&workspace){nav('workspace/'+workspace.id);return}
 if(parts[0]==='project'){await projectPage(parts[1],parts[2],parts[3]);return}
 if(parts[0]==='app'){await appPage(parts[1],parts[2]);return}
 if(parts[0]==='workspace'&&workspace){
  localStorage.setItem('workspace',workspace.id);
  if(parts[2]==='settings'){workspaceSettings();return}
  const [projects,allApps,allDeployments]=await Promise.all([api('projects?workspace_id='+workspace.id),api('containers'),api('releases?logs=false')]);
  if(version!==viewVersion)return;
  const apps=allApps.filter(a=>projects.some(p=>p.id===a.project_id));
  const deployments=allDeployments.filter(d=>projects.some(p=>p.id===d.project_id));
  const running=apps.filter(a=>a.status==='running').length;
  shell(heading('Workspace overview','Projects','One source and deployment per project.','<button id="new-project" class="primary">'+icon('plus')+'New project</button>')+
   '<div class="stats"><div class="stat"><div><span>Total projects</span><strong>'+projects.length+'</strong></div>'+icon('grid')+'</div><div class="stat"><div><span>Containers</span><strong>'+apps.length+'</strong></div>'+icon('box')+'</div><div class="stat stat-positive"><div><span>Running containers</span><strong>'+running+'</strong></div>'+icon('activity')+'</div></div>'+
   '<div class="workspace-layout"><section><div class="section-head"><h2>All projects<span class="count">'+projects.length+'</span></h2><label class="search-field">'+icon('search')+'<input type="search" id="project-search" placeholder="Find a project…" aria-label="Find a project"></label></div>'+
   (projects.length?'<div class="panel project-directory">'+projects.map(p=>collectionCard(p,'project',apps,deployments)).join('')+'</div><p class="note" id="no-project-matches" hidden>No projects match your search.</p>':empty('No projects yet','Create a project, then connect your first repository.'))+'</section>'+releasePanel(deployments,projects)+'</div>');
  $('#project-search').oninput=e=>{let matches=0;document.querySelectorAll('[data-project-name]').forEach(a=>{a.hidden=!a.dataset.projectName.includes(e.target.value.trim().toLowerCase());if(!a.hidden)matches++});if($('#no-project-matches'))$('#no-project-matches').hidden=matches>0};
  $('#new-project').onclick=createProject;return;
 }
 shell(heading('Infrastructure','Workspaces','Organize the projects running on your server.','<button class="primary" id="new-workspace">'+icon('plus')+'New workspace</button>')+
  '<div class="section-head"><h2>All workspaces<span class="count">'+workspaces.length+'</span></h2></div>'+
  (workspaces.length?'<div class="grid">'+workspaces.map(w=>collectionCard(w,'workspace')).join('')+'</div>':empty('No workspaces yet','Create a workspace to organize your projects.')));
 $('#new-workspace').onclick=createWorkspace;
 }catch(e){if(version===viewVersion)$('#root').innerHTML='<div class="error-page"><h1>Something went wrong.</h1><p>'+esc(e.message)+'</p><a class="button" href="#workspaces">Back to workspaces</a></div>'}
}
window.addEventListener('hashchange',render);render();
