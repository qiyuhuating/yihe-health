/* Server session stays in memory; cookies are managed by the browser. */
window.CareSession = (() => {
  'use strict';
  let current=null,checking=null;
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
    if(!checking)checking=request('/session',{authenticated:false}).then(accept).finally(()=>{checking=null;});
    return checking;
  }
  async function login(role,username,password){
    await read();
    const user=accept(await request('/session',{method:'POST',body:{role,username,password},authenticated:false}));
    if(user?.role!==role)throw failure('FORBIDDEN','该账号不能访问此入口，请使用对应的登录入口。');
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
    current=null;CareTransport.setCsrfToken('');
  }
  function staffGate(){
    const el=id=>document.getElementById(id),callbacks=[];
    let ready=null,pending=false;
    const nameField=el('adminLoginName'),passwordField=el('adminLoginPassword'),button=el('adminLoginSubmit'),label=button.querySelector('[data-login-label]'),originalLabel=label?.textContent;
    const clearHint=()=>{el('adminLoginHint').hidden=true;for(const field of [nameField,passwordField]){field.removeAttribute('aria-invalid');field.removeAttribute('aria-describedby');}};
    const hint=(message,invalidFields=[],focusField=null)=>{const node=el('adminLoginHint');node.textContent=message;node.hidden=false;for(const field of [nameField,passwordField])field.setAttribute('aria-describedby',node.id);for(const field of invalidFields)field.setAttribute('aria-invalid','true');(focusField||passwordField).focus();};
    function publish(user){
      if(user?.role!=='staff')return;
      ready=user;el('adminLoginOverlay').hidden=true;passwordField.value='';clearHint();
      callbacks.splice(0).forEach(cb=>Promise.resolve(cb(user)).catch(e=>hint(e.message)));
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
    return {onReady:cb=>{if(ready)Promise.resolve(cb(ready)).catch(e=>hint(e.message));else callbacks.push(cb);},getLogin:()=>ready,isLoggedIn:()=>!!ready};
  }
  window.addEventListener('care:unauthenticated',()=>{
    current=null;document.querySelector('#staffApp')?.setAttribute('hidden','');
    document.querySelector('main')?.setAttribute('inert','');
    document.querySelectorAll('dialog[open]').forEach(dialog=>dialog.close());
    if(document.querySelector('#staffApp'))location.reload();else if(!document.querySelector('#loginForm'))location.replace('login.html');
  });
  return {read,login,requireRole,logout,staffGate,get current(){return current;}};
})();
