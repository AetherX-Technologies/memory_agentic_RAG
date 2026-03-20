// 检查服务状态
async function checkHealth() {
  const statusEl = document.getElementById('status');

  try {
    const response = await fetch('http://localhost:8080/api/health');
    if (response.ok) {
      const data = await response.json();
      statusEl.className = 'status ok';
      statusEl.textContent = `✓ 服务正常 (v${data.version || '?'})`;
    } else {
      throw new Error(`HTTP ${response.status}`);
    }
  } catch (error) {
    statusEl.className = 'status error';
    statusEl.textContent = '✗ 服务离线';
  }
}

// 更新队列统计
async function updateStats() {
  try {
    const result = await chrome.storage.local.get('offline_queue');
    const queueSize = (result.offline_queue || []).length;
    document.getElementById('queue').textContent = queueSize;
  } catch (error) {
    document.getElementById('queue').textContent = '?';
  }
}

// 重试按钮
document.getElementById('retry').addEventListener('click', async () => {
  const btn = document.getElementById('retry');
  btn.disabled = true;
  btn.textContent = '重试中...';

  try {
    await chrome.runtime.sendMessage({ type: 'RETRY_QUEUE' });
    await updateStats();
  } catch (error) {
    console.error('[HybridMem] Retry failed:', error);
  } finally {
    btn.disabled = false;
    btn.textContent = '重试';
  }
});

// 初始化
checkHealth();
updateStats();
