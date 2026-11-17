const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function dashboard(deployments) {
  const elements = new Map();
  const buttons = deployments.map(d => ({dataset: {id: d.id}}));
  const content = {
    set innerHTML(html) {
      this.html = html;
      elements.delete('#release-history');
      if (html.includes('id="release-history"')) elements.set('#release-history', {scrollLeft: 0});
      elements.set('#build-log', {scrollTop: 0, scrollHeight: 100, clientHeight: 100});
      elements.set('#log-id', {});
    }
  };
  for (const id of ['#deploy', '#active-status']) elements.set(id, {});
  elements.set('#tab-content', content);
  const fixtures = {
    'apps/app': {id: 'app', name: 'Example', project_id: 'project', branch: 'main'},
    'projects/project': {id: 'project', name: 'Project'},
    'routes?project_id=project': [],
    'deployments?app_id=app': deployments,
    ...Object.fromEntries(deployments.map(d => ['deployments/' + d.id, d]))
  };
  let poll;
  const context = vm.createContext({
    fixtures,
    location: {hash: '#app/app'},
    document: {
      querySelector: selector => elements.get(selector),
      querySelectorAll: selector => selector === '.select-deploy' ? buttons : []
    },
    window: {addEventListener() {}},
    clearInterval() {},
    setInterval(callback) { poll = callback; return 1; }
  });
  const source = fs.readFileSync(path.join(__dirname, '../web/app.js'), 'utf8');
  vm.runInContext(source.slice(0, source.lastIndexOf('render();')), context);
  vm.runInContext('api = async path => fixtures[path]; shell = () => {};', context);
  return {context, elements, buttons, content, refresh: async () => {
    poll();
    await new Promise(setImmediate);
  }};
}

test('release history preserves horizontal scroll across polling and selection', async () => {
  const page = dashboard([
    {id: 'older', status: 'superseded', log: 'Older output'},
    {id: 'latest', status: 'running', log: 'Latest output'}
  ]);
  await vm.runInContext("appPage('app')", page.context);
  page.elements.get('#release-history').scrollLeft = 180;
  page.elements.get('#build-log').scrollHeight = 1000;
  page.elements.get('#build-log').scrollTop = 120;
  await page.refresh();
  assert.equal(page.elements.get('#release-history').scrollLeft, 180);
  assert.equal(page.elements.get('#build-log').scrollTop, 120);
  await page.buttons[0].onclick();
  assert.equal(page.elements.get('#release-history').scrollLeft, 180);
  assert.equal(page.elements.get('#build-log').textContent, 'Older output');
});

test('empty release history renders without a scrolling table', async () => {
  const page = dashboard([]);
  await vm.runInContext("appPage('app')", page.context);
  await page.refresh();
  assert.equal(page.elements.has('#release-history'), false);
  assert.match(page.content.html, /Ready when you are/);
});

test('project cards escape names and include an accessible text destination', () => {
  const page = dashboard([]);
  const html = vm.runInContext('projectCard({id: "safe", name: "<script>alert(1)</script>"})', page.context);
  assert.ok(!html.includes('<script>'));
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, /View project/);
});
