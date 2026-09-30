/* Same-origin JSON transport. No automatic retry of writes with unknown outcomes. */
window.CareTransport = (() => {
  'use strict';
  const config=window.YIHE_RUNTIME_CONFIG;
  const failure=(code,message,status)=>Object.assign(new Error(message),{name:'CareError',code,status});
  const messages={UNAUTHENTICATED:'登录已失效，请重新登录。',FORBIDDEN:'当前账号没有此项权限。',
    CONFLICT:'记录已被更新，请重新读取最新记录，核对后再提交。',VALIDATION_FAILED:'提交内容未通过校验，请检查后重试。',
    RATE_LIMITED:'请求过于频繁，请稍后再试。',SERVICE_UNAVAILABLE:'服务暂时不可用，请稍后重新读取。',
    REQUEST_FAILED:'请求未能完成，请重新读取后重试。'};
  const newIdempotencyKey=()=>crypto.randomUUID?crypto.randomUUID():Array.from(crypto.getRandomValues(new Uint8Array(16)),value=>value.toString(16).padStart(2,'0')).join('');
  let csrfToken='';
  async function request(path,{method='GET',body,authenticated=true,idempotencyKey}={}) {
    if(config.mode!=='http')throw failure('CONFIG_INVALID','当前未启用服务接口。');
    const controller=new AbortController(),write=method!=='GET';
    const timer=setTimeout(()=>controller.abort(),config.requestTimeoutMs);
    try {
      const headers={Accept:'application/json'};
      if(body!==undefined)headers['Content-Type']='application/json';
      if(idempotencyKey)headers['Idempotency-Key']=idempotencyKey;
      if(write){
        if(!csrfToken)throw failure('SESSION_REQUIRED','请重新登录后再提交。');
        headers['X-CSRF-Token']=csrfToken;
      }
      const response=await fetch(config.apiBaseUrl+path,{method,headers,body:body===undefined?undefined:JSON.stringify(body),
        credentials:'same-origin',cache:'no-store',redirect:'error',signal:controller.signal});
      if(!response.ok){
        const code=({401:'UNAUTHENTICATED',403:'FORBIDDEN',409:'CONFLICT',400:'VALIDATION_FAILED',422:'VALIDATION_FAILED',429:'RATE_LIMITED'})[response.status]
          ||(response.status>=500?'SERVICE_UNAVAILABLE':'REQUEST_FAILED');
        if(code==='UNAUTHENTICATED'&&authenticated){csrfToken='';window.dispatchEvent(new CustomEvent('care:unauthenticated'));}
        let message=messages[code];
        if(code==='VALIDATION_FAILED'&&response.headers.get('content-type')?.includes('application/json')){
          // Only fixed, public codes become UI text; never display arbitrary server prose.
          const publicErrors={NOTE_REQUIRED:'请填写处置记录后再提交。',INVALID_TIME:'请检查带时区的计划时间。',IDEMPOTENCY_REQUIRED:'请求缺少登记意图，请重新打开表单。',LIMIT_EXCEEDED:'未完成任务已达上限，请先处理现有任务。'};
          try{const value=await response.json();if(Object.hasOwn(publicErrors,value?.error?.code))message=publicErrors[value.error.code];}catch(_){/* Generic message remains safe. */}
        }
        throw failure(code,message,response.status);
      }
      if(response.status===204)return null;
      if(!response.headers.get('content-type')?.includes('application/json'))throw failure('INVALID_RESPONSE','服务返回的数据格式不正确，请联系维护人员。');
      let value;
      try {value=await response.json();}catch(e){if(controller.signal.aborted)throw e;throw failure('INVALID_RESPONSE','服务返回的数据无法解析。');}
      if(value&&typeof value==='object'&&Object.hasOwn(value,'data'))return value.data;
      return value;
    } catch(e) {
      if(e.name==='CareError'){
        if(write&&['SERVICE_UNAVAILABLE','INVALID_RESPONSE'].includes(e.code))e.message+=' 操作结果尚未确认，请先重新读取记录，避免重复提交。';
        throw e;
      }
      const outcome=write?'操作结果尚未确认，请先重新读取记录，确认是否已生效，避免重复提交。':'请检查网络后重新读取。';
      if(controller.signal.aborted)throw failure('REQUEST_TIMEOUT','请求超时。'+outcome);
      throw failure('NETWORK_ERROR','网络连接中断。'+outcome);
    } finally {clearTimeout(timer);}
  }
  return {request,failure,setCsrfToken:value=>{csrfToken=value;},newIdempotencyKey};
})();
