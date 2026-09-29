/* Product wording is set before page controllers initialize. */
(() => {
  if(window.YIHE_RUNTIME_CONFIG?.mode!=='http')return;
  document.querySelectorAll('[data-demo-only]').forEach(node=>{node.hidden=true;node.inert=true;});
  document.querySelectorAll('[data-http-only]').forEach(node=>{node.hidden=false;});
  document.querySelectorAll('[data-http-text]').forEach(node=>{node.textContent=node.dataset.httpText;});
})();
