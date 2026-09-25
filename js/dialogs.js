/* Focus management for existing overlay markup. */
(() => {
  const previous = new WeakMap();
  function focusables(root) { return [...root.querySelectorAll('button,input,select,textarea,a[href],[tabindex="0"]')].filter(x => !x.disabled && x.getClientRects().length); }
  document.querySelectorAll('.overlay,.sos-overlay,.login-overlay').forEach(dialog => {
    dialog.setAttribute('role', dialog.id === 'sosOverlay' ? 'alertdialog' : 'dialog');
    dialog.setAttribute('aria-modal','true');
    if (!dialog.hasAttribute('aria-labelledby')) {
      const title=dialog.querySelector('h2,.login-title');
      if (title) { title.id ||= `${dialog.id}-title`; dialog.setAttribute('aria-labelledby',title.id); }
    }
    new MutationObserver(() => {
      if (!dialog.hidden) { previous.set(dialog,document.activeElement); focusables(dialog)[0]?.focus(); }
      else { const el=previous.get(dialog); if(el?.isConnected) el.focus(); }
    }).observe(dialog,{attributes:true,attributeFilter:['hidden']});
    dialog.addEventListener('keydown',event => {
      if(event.key==='Escape' && !dialog.classList.contains('login-overlay')) {
        event.preventDefault();event.stopPropagation();
        const request=new CustomEvent('dialog:request-close',{cancelable:true});
        if(dialog.dispatchEvent(request)) dialog.hidden=true;
      }
      if(event.key!=='Tab') return;
      const list=focusables(dialog); if(!list.length) {event.preventDefault();return;}
      if(event.shiftKey && document.activeElement===list[0]) {event.preventDefault();list.at(-1).focus();}
      else if(!event.shiftKey && document.activeElement===list.at(-1)) {event.preventDefault();list[0].focus();}
    });
  });
})();
