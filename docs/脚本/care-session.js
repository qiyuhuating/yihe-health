/* Server session stays in memory; cookies are managed by the browser. */
window.CareSession = (() => {
  'use strict';
  let current=null,checking=null,idleTimer=null,lastActivity=0,idleRevocation=null,sessionGeneration=0;
  const IDLE_MS=15*60000;
  function expire(){current=null;clearTimeout(idleTimer);CareTransport.setCsrfToken('');window.dispatchEvent(new CustomEvent('care:unauthenticated'));}
  function checkIdle(){
    if(current?.role!=='staff')return;
    const remaining=IDLE_MS-(performance.now()-lastActivity);
    if(remaining<=0){
      idleRevocation=request('/session',{method:'DELETE',authenticated:false}).catch(()=>{
        const notice=document.getElementById('adminLoginHint');if(notice){notice.textContent='页面已锁定，但服务端退出结果未确认。请重新登录；草稿仍保留在本页面。';notice.hidden=false;}
      }).finally(()=>{idleRevocation=null;});
      expire();
    }
    else {clearTimeout(idleTimer);idleTimer=setTimeout(checkIdle,remaining);}
  }
  function activity(){if(current?.role==='staff'){lastActivity=performance.now();checkIdle();}}
  for(const type of ['pointerdown','keydown'])window.addEventListener(type,event=>{if(event.isTrusted)activity();});
  window.addEventListener('pageshow',checkIdle);
  window.addEventListener('visibilitychange',checkIdle);
  const {request,failure}=CareTransport;
  function accept(value){
    const user=value?.user;
    if(!value||typeof value.csrfToken!=='string'||!value.csrfToken||
      (user!==null&&(!user||typeof user.name!=='string'||!user.name||!['resident','staff'].includes(user.role)||
        (user.role==='resident'?!Number.isSafeInteger(user.residentId)||user.residentId<1:typeof user.staffId!=='string'||!user.staffId)))){
      throw failure('INVALID_RESPONSE','登录服务返回的会话格式无效。');
    }
    current=user;CareTransport.setCsrfToken(value.csrfToken);return user;
  }
  function read(){
    if(!checking){const generation=sessionGeneration;checking=request('/session',{authenticated:false}).then(value=>{if(generation!==sessionGeneration)throw failure('UNAUTHENTICATED','登录已失效，请重新登录。');return accept(value);}).finally(()=>{checking=null;});}
    return checking;
  }
  async function login(role,username,password){
    await idleRevocation;await read();
    const user=accept(await request('/session',{method:'POST',body:{role,username,password},authenticated:false}));
    if(user?.role!==role){
      try{await request('/session',{method:'DELETE',authenticated:false});}
      catch(_){current=null;CareTransport.setCsrfToken('');throw failure('SESSION_REVOKE_FAILED','账号入口不符，退出结果未确认。请重新读取会话后重试。');}
      current=null;CareTransport.setCsrfToken('');
      throw failure('FORBIDDEN','该账号不能访问此入口，已退出该会话。');
    }
    activity();
    return user;
  }
  async function requireRole(role){
    const user=await read();
    if(!user)throw failure('UNAUTHENTICATED','请先登录。');
    if(user.role!==role)throw failure('FORBIDDEN','该账号不能访问此入口，请切换账号。');
    return user;
  }
  async function logout(){
    await read();await request('/session',{method:'DELETE',authenticated:false});
    current=null;clearTimeout(idleTimer);CareTransport.setCsrfToken('');
  }
  function staffGate(){
    const el=id=>document.getElementById(id),callbacks=[];
    let ready=null,pending=false;
    const nameField=el('adminLoginName'),passwordField=el('adminLoginPassword'),button=el('adminLoginSubmit'),label=button.querySelector('[data-login-label]'),originalLabel=label?.textContent;
    const clearHint=()=>{el('adminLoginHint').hidden=true;for(const field of [nameField,passwordField]){field.removeAttribute('aria-invalid');field.removeAttribute('aria-describedby');}};
    const hint=(message,invalidFields=[],focusField=null)=>{const node=el('adminLoginHint');node.textContent=message;node.hidden=false;for(const field of [nameField,passwordField])field.setAttribute('aria-describedby',node.id);for(const field of invalidFields)field.setAttribute('aria-invalid','true');(focusField||passwordField).focus();};
    function publish(user){
      if(user?.role!=='staff')return;
      ready=user;activity();el('adminLoginOverlay').hidden=true;passwordField.value='';clearHint();
      callbacks.forEach(cb=>Promise.resolve(cb(user)).catch(e=>hint(e.message)));
      window.dispatchEvent(new CustomEvent('care:authenticated'));
    }
    async function submit(){
      if(pending)return;
      const name=nameField.value.trim(),password=passwordField.value;
      clearHint();
      if(!name||!password){const missing=!name?nameField:passwordField;hint(!name?'请输入账号':'请输入密码',[missing],missing);return;}
      pending=true;button.disabled=true;button.setAttribute('aria-busy','true');if(label)label.textContent='正在验证…';
      try{publish(await login('staff',name,password));}catch(e){hint(e.message,e.code==='UNAUTHENTICATED'?[nameField,passwordField]:[],passwordField);}finally{
        pending=false;button.disabled=false;button.removeAttribute('aria-busy');if(label)label.textContent=originalLabel;
      }
    }
    button.addEventListener('click',submit);
    nameField.addEventListener('input',clearHint);passwordField.addEventListener('input',clearHint);
    passwordField.addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();submit();}});
    nameField.addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();passwordField.focus();}});
    nameField.focus();
    el('btnAdminLogout').addEventListener('click',async()=>{
      el('btnAdminLogout').disabled=true;
      try{await logout();location.reload();}catch(e){el('careError').textContent='退出未完成：'+e.message;el('careError').hidden=false;}
      finally{el('btnAdminLogout').disabled=false;}
    });
    read().then(user=>{if(user&&user.role!=='staff')hint('当前是居民账号，请使用工作人员账号登录。');else publish(user);}).catch(e=>hint(e.message));
    window.addEventListener('care:unauthenticated',()=>{ready=null;el('adminLoginOverlay').hidden=false;hint('登录已失效；草稿仅保留在当前页面，请使用原账号重新登录。',[],nameField);});
    return {onReady:cb=>{callbacks.push(cb);if(ready)Promise.resolve(cb(ready)).catch(e=>hint(e.message));},getLogin:()=>ready,isLoggedIn:()=>!!ready};
  }
  let residentReauth=null;
  window.addEventListener('care:unauthenticated',()=>{
    sessionGeneration++;current=null;clearTimeout(idleTimer);CareTransport.setCsrfToken('');
    document.querySelector('#staffApp')?.setAttribute('hidden','');
    document.querySelector('main')?.setAttribute('inert','');
    document.querySelectorAll('dialog[open]').forEach(dialog=>dialog.close());
    if(document.querySelector('#staffApp')||document.querySelector('#loginForm'))return;
    if(!residentReauth){
      const dialog=document.createElement('dialog'),form=document.createElement('form'),heading=document.createElement('h2'),hint=document.createElement('p');
      dialog.className='care-dialog';heading.textContent='登录已失效';hint.textContent='草稿仅保留在当前页面，请使用原账号重新登录。';hint.setAttribute('role','alert');
      const name=document.createElement('input'),password=document.createElement('input'),button=document.createElement('button');
      name.className='input';password.className='input';button.className='btn btn-primary';name.autocomplete='username';password.type='password';password.autocomplete='current-password';button.textContent='重新登录';button.type='submit';
      form.append(heading,hint);
      for(const [label,field] of [['账号',name],['密码',password]]){const node=document.createElement('label');node.textContent=label;node.append(field);form.append(node);}
      form.append(button);dialog.append(form);document.body.append(dialog);
      dialog.addEventListener('cancel',event=>event.preventDefault());
      form.addEventListener('submit',async event=>{
        event.preventDefault();button.disabled=true;
        try{await login('resident',name.value.trim(),password.value);password.value='';dialog.close();window.dispatchEvent(new CustomEvent('care:authenticated'));}
        catch(e){hint.textContent=e.message;password.value='';}finally{button.disabled=false;}
      });
      residentReauth=dialog;
    }
    residentReauth.showModal();
  });
  return {read,login,requireRole,logout,staffGate,get current(){return current;}};
})();
