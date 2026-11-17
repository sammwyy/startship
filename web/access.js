'use strict';

// Users, roles, members and audit views; the shared dashboard shell owns navigation.
Object.assign(icons, {
 crown: '<path d="m3 8 4.5 4L12 5l4.5 7L21 8l-2 11H5Z"/>',
 wrench: '<path d="M14.7 6.3a4 4 0 0 0 5 5l-9.4 9.4a2.1 2.1 0 0 1-3-3Zm0 0L17 4l3 3-2.3 2.3"/>',
 eye: '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
 star: '<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1 6.2L12 17.3 6.5 20.2l1-6.2L3 9.6l6.2-.9Z"/>',
 bolt: '<path d="M13 2 4 14h7l-1 8 9-12h-7Z"/>',
 rocket: '<path d="M5 15c-1.5 1.5-2 5-2 5s3.5-.5 5-2m-1.5-4.5L12 19l2-4c4-2.5 6.5-6.5 7-12-5.5.5-9.5 3-12 7l-4 2Zm8.5-5.5h.01"/>',
 code: '<path d="m8 7-5 5 5 5m8-10 5 5-5 5M14 4l-4 16"/>',
 key: '<circle cx="8" cy="15" r="4"/><path d="m11 12 9-9m-3 3 3 3m-6 0 2 2"/>',
 flag: '<path d="M5 21V4m0 0h11l-2 4 2 4H5"/>',
 heart: '<path d="M12 20s-8-4.6-8-10.5A4.5 4.5 0 0 1 12 7a4.5 4.5 0 0 1 8 2.5C20 15.4 12 20 12 20Z"/>',
 trash: '<path d="M4 7h16M10 11v6m4-6v6M6 7l1 13h10l1-13M9 7V4h6v3"/>'
});
const roleColors=['#e5484d','#f76b15','#f5a524','#30a46c','#12a594','#3e63dd','#7c5cff','#d6409f','#8b8d98','#a18072'];
let permissionInfo=null,roleCache=null;
async function accessData(){
 if(!permissionInfo)permissionInfo=await api('permissions');
 roleCache=await api('roles');
 return {permissions:permissionInfo.permissions,icons:permissionInfo.icons,roles:roleCache};
}
const roleById=id=>(roleCache||[]).find(r=>r.id===id);
function roleIcon(role){return '<span class="role-icon" style="--role:'+esc(role?.color||'#8b8d98')+'">'+icon(role?.icon||'user')+'</span>'}
function roleChip(role,suffix='',remove=''){return '<span class="role-chip" style="--role:'+esc(role?.color||'#8b8d98')+'">'+icon(role?.icon||'user')+'<span>'+esc(role?.name||'Deleted role')+'</span>'+(suffix?'<small>'+esc(suffix)+'</small>':'')+(remove?'<button class="chip-remove" data-remove="'+esc(remove)+'" aria-label="Remove '+esc(role?.name||'role')+'" title="Remove">'+icon('close')+'</button>':'')+'</span>'}
function when(stamp){const d=new Date(stamp);return isNaN(d)?'':d.toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'})}
function grouped(permissions,filter=()=>true){const out=new Map();permissions.filter(filter).forEach(p=>{if(!out.has(p.group))out.set(p.group,[]);out.get(p.group).push(p)});return out}

async function rolesPage(id){
 const version=viewVersion,{permissions,icons:choices,roles}=await accessData();if(version!==viewVersion)return;
 const role=roles.find(r=>r.id===id)||roles[0],editable=!role.builtin&&can('roles.manage');
 const list='<nav class="panel role-list" aria-label="Roles">'+roles.map(r=>'<a href="#roles/'+esc(r.id)+'" '+(r.id===role.id?'aria-current="page"':'')+'>'+roleIcon(r)+'<span><strong>'+esc(r.name)+'</strong><small>'+r.members+' member'+(r.members===1?'':'s')+(r.builtin?' · Built-in':'')+'</small></span>'+(r.fixed?icon('lock'):'')+'</a>').join('')+'</nav>';
 const switches=[...grouped(permissions)].map(([group,items])=>'<fieldset class="permission-group"><legend>'+esc(group)+'</legend>'+items.map(p=>'<label class="permission-row"><span><strong>'+esc(p.label)+'</strong><small>'+esc(p.description)+'</small></span><input type="checkbox" role="switch" class="switch" name="perm" value="'+esc(p.key)+'" '+(role.permissions.includes(p.key)?'checked ':'')+(editable&&(p.scope!=='platform'||user.platform_admin)?'':'disabled')+'></label>').join('')+'</fieldset>').join('');
 const notice=role.fixed?'Platform Admin holds every permission everywhere. It cannot be edited or deleted, and at least one user always keeps it.':role.builtin?'Built-in roles are read-only. Clone one to start a custom role from its permissions.':!can('roles.manage')?'You need the Manage roles permission to change roles.':'';
 shell(heading('Platform','Roles','Name a role, pick a look and switch on what it can do.',can('roles.manage')?'<button id="clone-role">'+icon('copy')+'Clone</button><button class="primary" id="new-role">'+icon('plus')+'New role</button>':'')+
  '<div class="role-layout">'+list+'<form class="panel role-editor" id="role-editor"><div class="role-preview">'+roleIcon(role)+'<div><strong id="role-preview-name">'+esc(role.name)+'</strong><small>'+(role.fixed?'Fixed role':role.builtin?'Built-in role':'Custom role')+' · '+role.members+' member'+(role.members===1?'':'s')+'</small></div></div>'+
  (notice?'<div class="note role-note">'+icon('lock')+esc(notice)+'</div>':'')+
  '<div class="role-fields"><label>Role name<input name="name" value="'+esc(role.name)+'" maxlength="60" required '+(editable?'':'disabled')+'></label>'+
  '<div class="role-field"><span class="field-label">Color</span><div class="swatches" role="radiogroup" aria-label="Role color">'+roleColors.map(c=>'<button type="button" class="swatch" role="radio" data-color="'+c+'" style="--role:'+c+'" aria-checked="'+(c===role.color)+'" aria-label="'+c+'" '+(editable?'':'disabled')+'></button>').join('')+'<label class="swatch custom-color" title="Custom color"><input type="color" name="color" value="'+esc(role.color)+'" aria-label="Custom color" '+(editable?'':'disabled')+'></label></div></div>'+
  '<div class="role-field"><span class="field-label">Icon</span><div class="icon-picker" role="radiogroup" aria-label="Role icon">'+choices.map(n=>'<button type="button" role="radio" data-icon="'+n+'" aria-checked="'+(n===role.icon)+'" aria-label="'+n+'" title="'+n+'" '+(editable?'':'disabled')+'>'+icon(n)+'</button>').join('')+'</div></div></div>'+
  '<div class="permission-head"><h2>Permissions</h2><span class="count">'+role.permissions.length+'/'+permissions.length+'</span></div>'+switches+
  '<div class="form-error" role="alert"></div>'+(editable?'<div class="role-actions"><button type="button" class="danger" id="delete-role">'+icon('trash')+'Delete role</button><button class="primary" type="submit">Save changes</button></div>':'')+'</form></div>');
 const form=$('#role-editor');let color=role.color,shape=role.icon;
 const preview=()=>{form.querySelector('.role-preview .role-icon').style.setProperty('--role',color);form.querySelector('.role-preview .role-icon').innerHTML=icon(shape);$('#role-preview-name').textContent=form.name.value||'Untitled role';form.querySelectorAll('[data-color]').forEach(b=>b.setAttribute('aria-checked',b.dataset.color===color));form.querySelectorAll('[data-icon]').forEach(b=>b.setAttribute('aria-checked',b.dataset.icon===shape));form.querySelector('.permission-head .count').textContent=form.querySelectorAll('[name=perm]:checked').length+'/'+permissions.length};
 form.querySelectorAll('[data-color]').forEach(b=>b.onclick=()=>{color=b.dataset.color;form.color.value=color;preview()});
 form.color.oninput=()=>{color=form.color.value;preview()};
 form.querySelectorAll('[data-icon]').forEach(b=>b.onclick=()=>{shape=b.dataset.icon;preview()});
 form.name.oninput=preview;form.onchange=preview;
 const create=async body=>{const r=await api('roles',body);nav('roles/'+r.id);toast('Role created')};
 if($('#new-role'))$('#new-role').onclick=()=>create({name:'New role'}).catch(e=>toast(e.message));
 if($('#clone-role'))$('#clone-role').onclick=()=>create({clone_of:role.id}).catch(e=>toast(e.message));
 if(!editable)return;
 form.onsubmit=async e=>{e.preventDefault();const button=form.querySelector('[type=submit]');button.disabled=true;form.querySelector('.form-error').textContent='';
  try{await api('roles/'+role.id,{name:form.name.value,color,icon:shape,permissions:[...form.querySelectorAll('[name=perm]:checked')].map(i=>i.value)},'PATCH');await rolesPage(role.id);toast('Role saved')}catch(err){form.querySelector('.form-error').textContent=err.message}finally{button.disabled=false}};
 $('#delete-role').onclick=()=>modal('Delete '+role.name,'This removes the role from '+role.members+' member'+(role.members===1?'':'s')+' and drops its overrides. This cannot be undone.','','Delete role',async()=>{await api('roles/'+role.id,null,'DELETE');nav('roles');toast('Role deleted')});
}

async function membersPage(){
 const version=viewVersion;
 const [data,{permissions,roles},projects,users]=await Promise.all([api('workspaces/'+workspace.id+'/members'),accessData(),api('projects?workspace_id='+workspace.id),api('users')]);
 if(version!==viewVersion)return;
 const manage=data.can_manage,audit=manage?await api('audit?workspace_id='+workspace.id).catch(()=>[]):[];
 if(version!==viewVersion)return;
 const rows=data.members.map(m=>'<tr><td><span class="member-name"><span class="avatar">'+esc(m.username[0]?.toUpperCase())+'</span>'+esc(m.username)+(m.user_id===user.id?' <small>(you)</small>':'')+'</span></td><td><div class="chips">'+m.roles.map(a=>roleChip(roleById(a.role_id),a.scope==='project'?a.project_name:'Workspace',manage?a.id:'')).join('')+'</div></td></tr>').join('');
 shell(heading('Workspace','Members','Who can reach '+workspace.name+' and what their roles let them do.',manage?'<button class="primary" id="add-member">'+icon('plus')+'Add member</button>':'')+
  '<div class="panel table-panel"><table><thead><tr><th>Member</th><th>Roles</th></tr></thead><tbody>'+(rows||'<tr><td colspan="2">No members yet.</td></tr>')+'</tbody></table></div>'+
  '<p class="note">Workspace roles apply to every project. Guests belong to the workspace but see only the projects they are given a role in.</p>'+
  (manage?'<section class="override-section"><div class="section-head"><h2>Permission overrides</h2></div><form class="panel override-editor" id="override-editor"><div class="override-target"><label>Where<select name="scope"><option value="workspace:'+esc(workspace.id)+'">Whole workspace</option>'+projects.map(p=>'<option value="project:'+esc(p.id)+'">'+esc(p.name)+'</option>').join('')+'</select></label><label>Who<select name="subject"><optgroup label="Roles">'+roles.filter(r=>!r.fixed).map(r=>'<option value="role:'+esc(r.id)+'">'+esc(r.name)+'</option>').join('')+'</optgroup><optgroup label="Members">'+data.members.map(m=>'<option value="user:'+esc(m.user_id)+'">'+esc(m.username)+'</option>').join('')+'</optgroup></select></label></div><div id="override-rows"></div><div class="form-error" role="alert"></div><div class="role-actions"><span class="small">Deny removes a permission granted by roles; allow adds one. Project rules beat workspace rules and member rules beat role rules.</span><button class="primary" type="submit">Save overrides</button></div></form></section>'+
  '<section><div class="section-head"><h2>Recent changes</h2></div>'+auditTable(audit.slice(0,20))+'</section>':''));
 document.querySelectorAll('[data-remove]').forEach(b=>b.onclick=async()=>{try{await api('assignments/'+b.dataset.remove,null,'DELETE');await membersPage();toast('Role removed')}catch(e){toast(e.message)}});
 if(!manage)return;
 $('#add-member').onclick=()=>modal('Add member','Give someone a role in this workspace or in one of its projects.',
  '<label>User<select name="user_id" required>'+users.map(u=>'<option value="'+esc(u.id)+'">'+esc(u.username)+'</option>').join('')+'</select></label>'+
  '<label>Role<select name="role_id">'+roles.filter(r=>!r.fixed).map(r=>'<option value="'+esc(r.id)+'" '+(r.id==='member'?'selected':'')+'>'+esc(r.name)+'</option>').join('')+'</select></label>'+
  '<label>Scope<select name="scope"><option value="workspace:'+esc(workspace.id)+'">Whole workspace</option>'+projects.map(p=>'<option value="project:'+esc(p.id)+'">Project · '+esc(p.name)+'</option>').join('')+'</select></label>',
  'Add member',async f=>{const [scope,scope_id]=f.get('scope').split(':');await api('assignments',{user_id:f.get('user_id'),role_id:f.get('role_id'),scope,scope_id});await membersPage();toast('Member added')});
 const form=$('#override-editor');let current=[];
 const draw=async()=>{
  const [scope,scope_id]=form.scope.value.split(':'),[subject_type,subject_id]=form.subject.value.split(':');
  current=await api('overrides?scope='+scope+'&scope_id='+encodeURIComponent(scope_id));
  const o=current.find(v=>v.subject_type===subject_type&&v.subject_id===subject_id)||{allow:[],deny:[]};
  const allowed=permissions.filter(p=>p.scope==='project'||(scope==='workspace'&&p.scope==='workspace'));
  $('#override-rows').innerHTML=[...grouped(allowed)].map(([group,items])=>'<fieldset class="permission-group"><legend>'+esc(group)+'</legend>'+items.map(p=>{const state=o.deny.includes(p.key)?'deny':o.allow.includes(p.key)?'allow':'inherit';return '<div class="permission-row"><span><strong>'+esc(p.label)+'</strong><small>'+esc(p.description)+'</small></span><div class="tri-state" role="radiogroup" aria-label="'+esc(p.label)+'">'+[['deny','Deny','close'],['inherit','Inherit','settings'],['allow','Allow','plus']].map(([v,label,shape])=>'<label class="tri-'+v+'" title="'+label+'"><input type="radio" name="'+esc(p.key)+'" value="'+v+'" '+(state===v?'checked':'')+'>'+icon(shape)+'<span>'+label+'</span></label>').join('')+'</div></div>'}).join('')+'</fieldset>').join('');
 };
 form.scope.onchange=form.subject.onchange=()=>draw().catch(e=>toast(e.message));
 form.onsubmit=async e=>{e.preventDefault();const [scope,scope_id]=form.scope.value.split(':'),[subject_type,subject_id]=form.subject.value.split(':'),pick=v=>[...form.querySelectorAll('#override-rows input:checked')].filter(i=>i.value===v).map(i=>i.name);
  form.querySelector('.form-error').textContent='';try{await api('overrides',{scope,scope_id,subject_type,subject_id,allow:pick('allow'),deny:pick('deny')},'PUT');await draw();toast('Overrides saved')}catch(err){form.querySelector('.form-error').textContent=err.message}};
 await draw();
}

function auditTable(entries){
 return entries.length?'<div class="panel table-panel"><table class="audit-table"><thead><tr><th>When</th><th>Who</th><th>Change</th><th>Target</th></tr></thead><tbody>'+entries.map(e=>'<tr><td>'+esc(when(e.at))+'</td><td>'+esc(e.actor)+'</td><td><span class="badge audit-'+esc(e.action.split('.')[1])+'">'+esc(e.action.replace('.',' '))+'</span></td><td>'+esc(e.target)+(e.scope!=='platform'?' <small>'+esc(e.scope)+'</small>':'')+'</td></tr>').join('')+'</tbody></table></div>':empty('No changes yet','Changes to roles, members and overrides show up here.');
}

async function auditPage(){
 const version=viewVersion,entries=await api('audit');if(version!==viewVersion)return;
 shell(heading('Platform','Audit log','Every change to users, roles, members and overrides, with who made it.')+auditTable(entries));
}

async function usersPage(){
 const version=viewVersion,[users,{roles}]=await Promise.all([api('users'),accessData()]);if(version!==viewVersion)return;
 shell(heading('Platform','Users','Accounts on this server and the roles they hold across the platform.','<button class="primary" id="new-user">'+icon('plus')+'New user</button>')+
  '<div class="panel table-panel"><table><thead><tr><th>User</th><th>Platform roles</th><th></th></tr></thead><tbody>'+users.map(u=>'<tr><td><span class="member-name"><span class="avatar">'+esc(u.username[0]?.toUpperCase())+'</span>'+esc(u.username)+(u.id===user.id?' <small>(you)</small>':'')+(u.must_change_password?' <span class="badge queued">Password pending</span>':'')+'</span></td><td><div class="chips">'+(u.platform_roles||[]).map(r=>roleChip(roleById(r),'',u.id+':'+r)).join('')+'<button class="chip-add" data-grant="'+esc(u.id)+'" aria-label="Add a platform role to '+esc(u.username)+'">'+icon('plus')+'Add role</button></div></td><td class="row-actions"><button data-reset="'+esc(u.id)+'" data-name="'+esc(u.username)+'">'+icon('key')+'Reset password</button>'+(u.id===user.id?'':'<button class="danger" data-delete="'+esc(u.id)+'" data-name="'+esc(u.username)+'">'+icon('trash')+'Delete</button>')+'</td></tr>').join('')+'</tbody></table></div>'+
  '<p class="note">Users start with no platform permissions: they can only edit their own account until a role or a workspace membership gives them more.</p>');
 const reload=()=>usersPage();
 $('#new-user').onclick=()=>modal('New user','They choose their own password the first time they sign in.',field('Username','username','','text','required maxlength="100" autocomplete="off"')+field('Temporary password','password','','password','required minlength="8" maxlength="72" autocomplete="new-password"'),'Create user',async f=>{await api('users',Object.fromEntries(f));await reload();toast('User created')});
 document.querySelectorAll('[data-reset]').forEach(b=>b.onclick=()=>modal('Reset password for '+b.dataset.name,'Signs them out everywhere; they must choose a new password at the next sign-in.',field('Temporary password','password','','password','required minlength="8" maxlength="72" autocomplete="new-password"'),'Reset password',async f=>{await api('users/'+b.dataset.reset,{password:f.get('password')},'PATCH');toast('Password reset')}));
 document.querySelectorAll('[data-delete]').forEach(b=>b.onclick=()=>modal('Delete '+b.dataset.name,'Removes the account, its sessions and every role it holds. Workspaces and projects stay.','','Delete user',async()=>{await api('users/'+b.dataset.delete,null,'DELETE');await reload();toast('User deleted')}));
 document.querySelectorAll('[data-grant]').forEach(b=>b.onclick=()=>modal('Add platform role','Platform roles apply everywhere on this server.','<label>Role<select name="role_id">'+roles.filter(r=>r.id!=='platform-admin'||user.platform_admin).map(r=>'<option value="'+esc(r.id)+'">'+esc(r.name)+'</option>').join('')+'</select></label>','Add role',async f=>{await api('assignments',{user_id:b.dataset.grant,role_id:f.get('role_id'),scope:'platform',scope_id:''});await reload();toast('Role added')}));
 document.querySelectorAll('[data-remove]').forEach(b=>b.onclick=async()=>{const [uid,role]=b.dataset.remove.split(':');try{const [a]=await api('assignments?user_id='+uid+'&scope=platform').then(l=>l.filter(v=>v.role_id===role));await api('assignments/'+a.id,null,'DELETE');await reload();toast('Role removed')}catch(e){toast(e.message)}});
}

const domainPatterns=d=>[d.apex&&d.name,d.subdomains&&'*.'+d.name].filter(Boolean);
async function workspaceDomains(){
 const box=$('#workspace-domains');if(!box)return;
 const {domains,can_manage:manage}=await api('workspaces/'+workspace.id+'/domains');
 box.innerHTML='<div class="section-head"><div><h2>Domains</h2><p class="small muted">Projects in this workspace can only route hostnames under these domains.</p></div></div>'+
  (domains.length?'<div class="panel table-panel"><table class="domain-table"><thead><tr><th>Domain</th><th>Allows</th><th>Routes</th><th></th></tr></thead><tbody>'+domains.map(d=>'<tr><td><span class="domain-name">'+icon('globe')+esc(d.name)+'</span></td><td><div class="chips">'+domainPatterns(d).map(p=>'<code class="domain-pattern">'+esc(p)+'</code>').join('')+'</div></td><td>'+d.routes+'</td><td class="row-actions">'+(manage?'<button class="danger" data-delete-domain="'+esc(d.id)+'" data-name="'+esc(d.name)+'">'+icon('trash')+'Remove</button>':'')+'</td></tr>').join('')+'</tbody></table></div>':empty('No domains yet',manage?'Add a domain so projects can connect hostnames under it.':'A workspace administrator adds the domains projects can use.'))+
  (manage?'<form class="panel panel-pad domain-form" id="add-domain"><label>Domain<input name="name" required placeholder="example.com or *.example.com" autocomplete="off" spellcheck="false"></label><label class="inline-check"><input type="checkbox" name="subdomains">Also allow any subdomain</label><button class="primary" type="submit">'+icon('plus')+'Add domain</button><div class="form-error" role="alert"></div></form>':'');
 if(!manage)return;
 const form=$('#add-domain');
 form.onsubmit=async e=>{e.preventDefault();form.querySelector('.form-error').textContent='';try{await api('workspaces/'+workspace.id+'/domains',{name:form.name.value,subdomains:form.subdomains.checked});await workspaceDomains();toast('Domain added')}catch(err){form.querySelector('.form-error').textContent=err.message}};
 box.querySelectorAll('[data-delete-domain]').forEach(b=>b.onclick=()=>modal('Remove '+b.dataset.name,'Projects will no longer be able to connect hostnames under this domain.','','Remove domain',async()=>{await api('workspaces/'+workspace.id+'/domains/'+b.dataset.deleteDomain,null,'DELETE');await workspaceDomains();toast('Domain removed')}));
}

function hostField(domains,host=''){
 const exact=domains.find(d=>d.apex&&d.name===host),wild=!exact&&domains.find(d=>d.subdomains&&host.endsWith('.'+d.name));
 const custom=user.platform_admin,selected=exact?exact.name:wild?'*.'+wild.name:host&&custom?'':domains.length?(domains[0].subdomains?'*.':'')+domains[0].name:'';
 const options=domains.flatMap(d=>domainPatterns(d)).map(p=>'<option value="'+esc(p)+'" '+(p===selected?'selected':'')+'>'+esc(p)+'</option>').join('')+(custom?'<option value="" '+(selected===''?'selected':'')+'>Any hostname (Platform Admin)</option>':'');
 if(!options)return '<p class="note">This workspace has no domains yet. A workspace administrator adds them in Workspace settings.</p>';
 return '<div class="host-picker"><span class="field-label">Hostname</span><div class="host-composer"><input name="sub" aria-label="Subdomain" placeholder="app" value="'+esc(wild?host.slice(0,-wild.name.length-1):'')+'" autocomplete="off" spellcheck="false"><span class="host-dot">.</span><select name="domain" aria-label="Domain">'+options+'</select><input name="host" aria-label="Hostname" placeholder="example.com" value="'+esc(!exact&&!wild?host:'')+'" autocomplete="off" spellcheck="false"></div></div>';
}
function bindHostField(form){
 const select=form.querySelector('[name=domain]');if(!select)return;
 const sync=()=>{const v=select.value,box=form.querySelector('.host-composer');box.dataset.mode=v===''?'custom':v.startsWith('*.')?'sub':'exact';form.querySelector('[name=sub]').required=box.dataset.mode==='sub';form.querySelector('[name=host]').required=box.dataset.mode==='custom'};
 select.onchange=sync;sync();
}
function pickedHost(f){
 const v=f.get('domain');if(v===null)return '';
 return v===''?f.get('host'):v.startsWith('*.')?f.get('sub').trim().replace(/\.$/,'')+'.'+v.slice(2):v;
}
