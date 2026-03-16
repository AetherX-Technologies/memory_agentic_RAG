// 检查服务状态
async function checkHealth() {
  const statusEl = document.getElementById('status');

  try {
    const response = await fetch('http://localhost:8080/api/health');
    if (response.ok) {
      const data = await response.json();
      statusEl.className = 'status ok';
      statusEl.textContent = `✓ 服务正常 (v${data.version})`;
    } else {
      throw new Error('Service error');
    }
  } catch (error) {
    statusEl.className = 'status error';
    statusEl.textContent = '✗ 服务离线';
  }
}

// 更新队列统计
async function updateStats() {
  const cached = await chrome.storage.local.get('offline_queue');
  const queueSize = cached.offline_queue?.length || 0;
  document.getElementById('queue').textContent = queueSize;
}

// 重试按钮
document.getElementById('retry').addEventListener('click', async () => {
  await chrome.runtime.sendMessage({ type: 'RETRY_QUEUE' });
  await updateStats();
});

// 初始化
checkHealth();
updateStats();
