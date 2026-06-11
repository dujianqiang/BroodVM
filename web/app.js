const { createApp, ref, onMounted } = Vue;

createApp({
  setup() {
    const view = ref('loading');
    const user = ref(null);

    const loginForm = ref({ username: '', password: '' });
    const loginError = ref('');
    const loginLoading = ref(false);

    const vms = ref([]);

    const newForm = ref({ Name: '', VCPU: 2, MemoryGB: 4, DiskGB: 20, NetworkType: 'nat' });
    const creating = ref(false);
    const createProgress = ref({ visible: false, value: 0, message: '' });

    const currentVM = ref(null);

    // 当前正在执行的操作（start/stop/restart）
    const actionTask = ref(null); // { label, message, progress }

    // ---- API ----

    async function request(method, path, body) {
      const opts = { method, credentials: 'same-origin', headers: {} };
      if (body !== undefined) {
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(body);
      }
      const res = await fetch('/api' + path, opts);
      if (res.status === 401) {
        navigate('/login');
        throw Object.assign(new Error('未登录'), { status: 401 });
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || res.statusText);
      }
      if (res.status === 204) return null;
      return res.json();
    }

    // ---- 路由 ----

    function navigate(path) {
      location.hash = '#' + path;
    }

    async function route() {
      const hash = location.hash.replace(/^#/, '') || '/';
      if (hash === '/login') { view.value = 'login'; return; }
      if (!user.value) { navigate('/login'); return; }
      if (hash === '/') {
        try { vms.value = await request('GET', '/vms'); } catch { return; }
        view.value = 'list';
        return;
      }
      if (hash === '/vms/new') {
        newForm.value = { Name: '', VCPU: 2, MemoryGB: 4, DiskGB: 20, NetworkType: 'nat' };
        creating.value = false;
        createProgress.value = { visible: false, value: 0, message: '' };
        view.value = 'new';
        return;
      }
      const m = hash.match(/^\/vms\/([^/]+)$/);
      if (m) {
        try { currentVM.value = await request('GET', '/vms/' + m[1]); } catch { return; }
        view.value = 'vm-detail';
        return;
      }
      navigate('/');
    }

    // ---- 样式 ----

    function badgeStyle(s) {
      const c = { running: 'green', stopped: 'gray', creating: '#0073e6', error: 'red' };
      return 'background:' + (c[s] || 'gray');
    }

    // ---- 认证 ----

    async function login() {
      loginLoading.value = true;
      loginError.value = '';
      try {
        const r = await request('POST', '/login', loginForm.value);
        user.value = r.username;
        navigate('/');
      } catch (e) {
        if (e.status !== 401) loginError.value = e.message || '用户名或密码错误';
        else loginError.value = '用户名或密码错误';
      } finally {
        loginLoading.value = false;
      }
    }

    async function logout() {
      await request('POST', '/logout').catch(() => {});
      user.value = null;
      navigate('/login');
    }

    // ---- VM 操作 ----

    const actionLabels = { start: '启动', stop: '关机', restart: '重启' };

    async function vmAction(id, action) {
      const label = actionLabels[action] || action;
      actionTask.value = { label, message: '正在提交...', progress: 0 };
      try {
        const r = await request('POST', `/vms/${id}/${action}`);
        pollTask(r.task_id, t => {
          actionTask.value = { label, message: t.Message, progress: t.Progress };
        }, async t => {
          actionTask.value = null;
          if (t.Status === 'failed') alert(`${label}失败：` + t.Message);
          await route();
        });
      } catch (e) {
        actionTask.value = null;
        if (e.status !== 401) alert('操作失败: ' + e.message);
      }
    }

    async function confirmDelete(id) {
      if (!confirm('确认删除？')) return;
      try {
        const r = await request('DELETE', '/vms/' + id);
        alert('删除任务已提交，正在后台执行');
        pollTask(r.task_id, null, () => route());
      } catch (e) {
        if (e.status !== 401) alert('删除失败: ' + e.message);
      }
    }

    async function createVM() {
      creating.value = true;
      createProgress.value = { visible: false, value: 0, message: '' };
      try {
        const r = await request('POST', '/vms', newForm.value);
        createProgress.value = { visible: true, value: 0, message: '任务已提交...' };
        pollTask(r.task_id, t => {
          createProgress.value = { visible: true, value: t.Progress, message: t.Message };
        }, t => {
          if (t.Status === 'success') {
            navigate('/');
          } else {
            alert('创建失败：' + t.Message);
            creating.value = false;
          }
        });
      } catch (e) {
        if (e.status !== 401) alert('请求失败: ' + e.message);
        creating.value = false;
      }
    }

    function pollTask(taskId, onProgress, done) {
      const iv = setInterval(async () => {
        try {
          const t = await request('GET', '/tasks/' + taskId);
          onProgress && onProgress(t);
          if (t.Status === 'success' || t.Status === 'failed') {
            clearInterval(iv);
            done(t);
          }
        } catch {
          clearInterval(iv);
          done({ Status: 'failed', Message: '网络错误' });
        }
      }, 2000);
    }

    // ---- 初始化 ----

    onMounted(async () => {
      window.addEventListener('hashchange', () => route());
      try {
        const r = await request('GET', '/me');
        if (r.username) user.value = r.username;
      } catch {}
      await route();
    });

    return {
      view, user,
      loginForm, loginError, loginLoading,
      vms,
      newForm, creating, createProgress,
      currentVM, actionTask,
      badgeStyle, login, logout,
      vmAction, confirmDelete, createVM,
    };
  },
}).mount('#app');
