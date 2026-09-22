/* The menu delegates to ChatModule's controls instead of duplicating its state. */
(() => {
  const button=document.getElementById('chatMore');if(!button)return;
  const menu=document.createElement('div');menu.className='chat-more-menu';menu.hidden=true;
  for(const [label,id] of [['搜索消息','chatSearchBtn'],['导出本机记录','chatExport'],['多选消息','chatBatch']]){
    const item=document.createElement('button');item.type='button';item.textContent=label;item.addEventListener('click',()=>{document.getElementById(id).click();menu.hidden=true;button.setAttribute('aria-expanded','false');});menu.append(item);
  }
  button.after(menu);button.setAttribute('aria-expanded','false');
  button.addEventListener('click',()=>{menu.hidden=!menu.hidden;button.setAttribute('aria-expanded',String(!menu.hidden));if(!menu.hidden)menu.querySelector('button').focus();});
  document.addEventListener('click',event=>{if(!menu.contains(event.target)&&event.target!==button){menu.hidden=true;button.setAttribute('aria-expanded','false');}});
  document.addEventListener('keydown',event=>{if(event.key==='Escape'&&!menu.hidden){menu.hidden=true;button.focus();}});
})();
