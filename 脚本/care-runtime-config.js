/* Runtime selection is public deployment configuration, never a place for secrets. */
(() => {
  'use strict';
  const settings=Object.assign({mode:'demo',apiBaseUrl:'/api/v1',requestTimeoutMs:15000,pollIntervalMs:15000},window.YIHE_RUNTIME_CONFIG_OVERRIDE||{});
  window.YIHE_RUNTIME_CONFIG={mode:'invalid'};
  try {
    if(!['demo','http'].includes(settings.mode)||typeof settings.apiBaseUrl!=='string'||!/^\/(?!\/)[A-Za-z0-9/_-]+$/.test(settings.apiBaseUrl)||
      !Number.isInteger(settings.requestTimeoutMs)||settings.requestTimeoutMs<100||settings.requestTimeoutMs>120000||
      !Number.isInteger(settings.pollIntervalMs)||settings.pollIntervalMs<1000||settings.pollIntervalMs>300000)throw Error('服务配置无效，请联系维护人员。');
    settings.apiBaseUrl=settings.apiBaseUrl.replace(/\/$/,'');
    window.YIHE_RUNTIME_CONFIG=Object.freeze(settings);
  } catch(e) {
    const message=document.createElement('p');message.setAttribute('role','alert');message.textContent=e.message;document.body.prepend(message);
    document.querySelectorAll('main,form,#staffApp').forEach(node=>node.inert=true);
    throw e;
  }
})();
