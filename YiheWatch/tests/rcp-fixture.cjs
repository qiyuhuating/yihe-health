// SDK boundary only. Native UI acceptance separately proves real RCP cancel.
class Request {
  constructor(url, method, headers, content) { Object.assign(this, { url, method, headers, content }); }
}
const reply = (statusCode, body) => ({ statusCode, toString: () => JSON.stringify(body) });
module.exports = { Request, reply };
