window.CareLoginHttp = (() => {
  function attach() {
    const form=document.getElementById('loginForm'),button=document.getElementById('loginSubmit');
    const name=document.getElementById('loginName'),password=document.getElementById('loginPassword');
    const hint=document.getElementById('loginHint');
    let pending=false;
    const label=button.querySelector('[data-login-label]'),originalLabel=label?.textContent;
    const clearErrors=()=>{hint.hidden=true;for(const field of [name,password]){field.removeAttribute('aria-invalid');field.removeAttribute('aria-describedby');}};
    const showError=(message,invalidFields=[],focusField=null)=>{
      hint.textContent=message;hint.hidden=false;
      for(const field of [name,password])field.setAttribute('aria-describedby',hint.id);
      for(const field of invalidFields)field.setAttribute('aria-invalid','true');
      (focusField||password).focus();
    };
    for(const field of [name,password]){
      field.addEventListener('input',clearErrors);
    }
    async function submit() {
      if(pending)return;
      const username=name.value.trim(),secret=password.value;
      clearErrors();
      if(!username||!secret){const fields=[!username?name:null,!secret?password:null].filter(Boolean);showError('请输入账号和密码',fields,fields[0]);return;}
      pending=true;button.disabled=true;button.setAttribute('aria-busy','true');if(label)label.textContent='正在验证…';
      try {
        await CareSession.login('resident',username,secret);
        password.value='';location.replace('index.html');
      } catch(e) {showError(e.message||'登录服务暂时不可用，请稍后重试');}
      finally {pending=false;button.disabled=false;button.removeAttribute('aria-busy');if(label)label.textContent=originalLabel;}
    }
    form.addEventListener('submit',event=>{event.preventDefault();submit();});
    button.addEventListener('click',event=>{event.preventDefault();submit();});
    name.addEventListener('keydown',event=>{if(event.key==='Enter'){event.preventDefault();password.focus();}});
  }
  return {attach};
})();
