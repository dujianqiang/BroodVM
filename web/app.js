const API = {
  login:   (u, p)  => $.post('/api/login',   JSON.stringify({username:u,password:p}), null, 'json'),
  logout:  ()      => $.post('/api/logout'),
  vms:     ()      => $.get('/api/vms'),
  vm:      (id)    => $.get(`/api/vms/${id}`),
  create:  (data)  => $.ajax({url:'/api/vms', method:'POST', contentType:'application/json', data:JSON.stringify(data)}),
  delete:  (id)    => $.ajax({url:`/api/vms/${id}`, method:'DELETE'}),
  start:   (id)    => $.post(`/api/vms/${id}/start`),
  stop:    (id)    => $.post(`/api/vms/${id}/stop`),
  restart: (id)    => $.post(`/api/vms/${id}/restart`),
  task:    (tid)   => $.get(`/api/tasks/${tid}`),
};

const badge = s => {
  const color = {running:'green',stopped:'gray',creating:'blue',error:'red'}[s] || 'gray';
  return `<span class="badge" style="background:${color}">${s}</span>`;
};

function pollTask(taskId, onProgress, done) {
  const interval = setInterval(() => {
    API.task(taskId).done(t => {
      onProgress && onProgress(t);
      if (t.status === 'success' || t.status === 'failed') {
        clearInterval(interval);
        done(t);
      }
    }).fail(() => { clearInterval(interval); done({status:'failed',message:'网络错误'}); });
  }, 2000);
}

function renderLogin() {
  $('#app').html(`
    <article style="max-width:400px;margin:80px auto">
      <h2>登录 BroodVM</h2>
      <form id="form-login">
        <label>用户名<input id="inp-user" type="text" required></label>
        <label>密码<input id="inp-pass" type="password" required></label>
        <button type="submit">登录</button>
        <p id="login-err" style="color:red"></p>
      </form>
    </article>
  `);
  $('#form-login').on('submit', e => {
    e.preventDefault();
    API.login($('#inp-user').val(), $('#inp-pass').val())
      .done(() => navigate('/'))
      .fail(() => $('#login-err').text('用户名或密码错误'));
  });
}

function renderList() {
  API.vms().done(vms => {
    if (!vms || !vms.length) {
      $('#app').html('<p>暂无虚拟机 <a href="#/vms/new">立即创建</a></p>');
      return;
    }
    const rows = vms.map(v => `
      <tr>
        <td>${v.Name}</td>
        <td>${v.VCPU}</td>
        <td>${v.MemoryGB} GiB</td>
        <td>${v.DiskGB} GiB</td>
        <td>${v.IP || '-'}</td>
        <td>${badge(v.Status)}</td>
        <td>
          <button class="btn-start outline secondary" data-id="${v.ID}" ${v.Status==='running'?'disabled':''}>启动</button>
          <button class="btn-stop outline secondary"  data-id="${v.ID}" ${v.Status!=='running'?'disabled':''}>停止</button>
          <button class="btn-restart outline secondary" data-id="${v.ID}" ${v.Status!=='running'?'disabled':''}>重启</button>
          <button class="btn-del contrast outline" data-id="${v.ID}">删除</button>
        </td>
      </tr>`).join('');
    $('#app').html(`
      <table>
        <thead><tr><th>名称</th><th>vCPU</th><th>内存</th><th>磁盘</th><th>IP</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    `);
  }).fail(handleAuthFail);

  $('#app')
    .on('click', '.btn-start',   e => vmAction($(e.currentTarget).data('id'), 'start'))
    .on('click', '.btn-stop',    e => vmAction($(e.currentTarget).data('id'), 'stop'))
    .on('click', '.btn-restart', e => vmAction($(e.currentTarget).data('id'), 'restart'))
    .on('click', '.btn-del',     e => { if(confirm('确认删除？')) vmDelete($(e.currentTarget).data('id')); });
}

function vmAction(id, action) {
  API[action](id).done(() => renderList()).fail(r => alert(r.responseJSON?.error || '操作失败'));
}

function vmDelete(id) {
  API.delete(id).done(r => {
    alert('删除任务已提交，正在后台执行');
    pollTask(r.task_id, null, () => renderList());
  }).fail(r => alert(r.responseJSON?.error || '删除失败'));
}

function renderNew() {
  $('#app').html(`
    <article style="max-width:500px">
      <h2>新建虚拟机</h2>
      <form id="form-create">
        <label>名称<input id="inp-name" type="text" placeholder="vm-001" required></label>
        <label>vCPU<input id="inp-vcpu" type="number" value="2" min="1" required></label>
        <label>内存 (GiB)<input id="inp-mem" type="number" value="4" min="1" required></label>
        <label>磁盘 (GiB)<input id="inp-disk" type="number" value="20" min="10" required></label>
        <label>网络类型
          <select id="inp-net">
            <option value="nat">NAT</option>
            <option value="bridge">Bridge</option>
          </select>
        </label>
        <button type="submit">创建</button>
        <a href="#/" role="button" class="secondary outline">取消</a>
      </form>
      <div id="progress-area" style="display:none">
        <progress id="prog-bar" value="0" max="100"></progress>
        <p id="prog-msg"></p>
      </div>
    </article>
  `);
  $('#form-create').on('submit', e => {
    e.preventDefault();
    const data = {
      Name: $('#inp-name').val(), VCPU: +$('#inp-vcpu').val(),
      MemoryGB: +$('#inp-mem').val(), DiskGB: +$('#inp-disk').val(),
      NetworkType: $('#inp-net').val(),
    };
    $('button[type=submit]').prop('disabled', true);
    API.create(data).done(r => {
      $('#progress-area').show();
      pollTask(r.task_id, t => {
        $('#prog-bar').val(t.progress);
        $('#prog-msg').text(t.message);
      }, t => {
        if (t.status === 'success') navigate('/');
        else { alert('创建失败：' + t.message); $('button[type=submit]').prop('disabled', false); }
      });
    }).fail(r => {
      alert(r.responseJSON?.error || '请求失败');
      $('button[type=submit]').prop('disabled', false);
    });
  });
}

function renderVM(id) {
  API.vm(id).done(vm => {
    $('#app').html(`
      <article>
        <h2>${vm.Name}</h2>
        <table>
          <tr><th>ID</th><td>${vm.ID}</td></tr>
          <tr><th>状态</th><td>${badge(vm.Status)}</td></tr>
          <tr><th>vCPU</th><td>${vm.VCPU}</td></tr>
          <tr><th>内存</th><td>${vm.MemoryGB} GiB</td></tr>
          <tr><th>磁盘</th><td>${vm.DiskGB} GiB</td></tr>
          <tr><th>网络</th><td>${vm.NetworkType}</td></tr>
          <tr><th>IP</th><td>${vm.IP || '—'}</td></tr>
          <tr><th>VNC 端口</th><td>${vm.VNCPort}</td></tr>
          <tr><th>创建时间</th><td>${vm.CreatedAt}</td></tr>
        </table>
        <div class="grid">
          <button id="btn-start"   ${vm.Status==='running'?'disabled':''}>启动</button>
          <button id="btn-stop"    ${vm.Status!=='running'?'disabled':''}>停止</button>
          <button id="btn-restart" ${vm.Status!=='running'?'disabled':''}>重启</button>
          <button id="btn-del" class="contrast">删除</button>
        </div>
        <a href="#/">← 返回列表</a>
      </article>
    `);
    $('#btn-start').click(()   => vmAction(id, 'start'));
    $('#btn-stop').click(()    => vmAction(id, 'stop'));
    $('#btn-restart').click(() => vmAction(id, 'restart'));
    $('#btn-del').click(() => { if(confirm('确认删除？')) vmDelete(id); });
  }).fail(handleAuthFail);
}

function handleAuthFail(xhr) {
  if (xhr.status === 401) navigate('/login');
  else alert('请求失败: ' + xhr.status);
}

function navigate(path) { location.hash = '#' + path; }

function route() {
  const hash = location.hash.replace(/^#/, '') || '/';
  if (hash === '/login')     return renderLogin();
  if (hash === '/')          return renderList();
  if (hash === '/vms/new')   return renderNew();
  const m = hash.match(/^\/vms\/([^/]+)$/);
  if (m) return renderVM(m[1]);
  navigate('/');
}

$(document).ready(() => {
  $('#btn-logout').on('click', () => API.logout().always(() => navigate('/login')));
  $(window).on('hashchange', route);
  route();
});
