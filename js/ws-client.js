var WSClient = { on: function(event, callback) { if(event === 'fallback') setTimeout(callback,0); }, reconnect: function() {} };
