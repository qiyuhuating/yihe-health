(() => {
  function init(){
    document.querySelectorAll('[data-inline-style]').forEach(el=>{el.style.cssText=el.dataset.inlineStyle;el.removeAttribute('data-inline-style');});
    document.querySelectorAll('img').forEach(img=>{
      const key=img.dataset.icon||img.dataset.meIcon||img.dataset.secIcon||img.dataset.ctxIcon||img.dataset.modalIcon||img.dataset.vitalIcon||(img.hasAttribute('data-theme-icon')?'moon':null);
      if(key&&typeof iconSVG==='function')img.src=iconSVG(key,Number(img.getAttribute('width'))||20,'#365B49');
    });
    document.querySelectorAll('input,textarea,select').forEach(el=>{
      if(el.labels?.length||el.hasAttribute('aria-label'))return;
      const name=el.closest('.fg,.fg-col')?.querySelector('.fg-tit')?.textContent||el.placeholder||el.id;
      if(name)el.setAttribute('aria-label',name);
    });
    document.querySelectorAll('button').forEach(b=>{if(!b.hasAttribute('type'))b.type='button';});
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',init);else init();
})();
