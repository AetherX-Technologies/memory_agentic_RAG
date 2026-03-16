// API 配置
const API_BASE = 'http://localhost:8080';
const RETRY_DELAYS = [1000, 2000, 5000];

// 消息处理
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'CAPTURE_MESSAGE') {
    handleMessage(message.data);
  }
});

// 处理捕获的消息
async function handleMessage(data) {
  const memory = {
    text: data.text,
    category: 'fact',
    scope: `browser:${data.platform}`,
    importance: 0.5,
    timestamp: data.timestamp,
    metadata: JSON.stringify({
      source: 'browser-plugin',
      url: data.url,
      role: data.role,
      platform: data.platform,
      messageId: generateMessageId(data)
    })
  };

  await sendToAPI(memory);
}

// 发送到 API
async function sendToAPI(memory, retryCount = 0) {
  try {
    const response = await fetch(`${API_BASE}/api/memories`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(memory)
    });

    if (response.status === 400) {
      console.error('[HybridMem] Bad request:', await response.text());
      return;
    }

    if (response.status === 500 && retryCount < RETRY_DELAYS.length) {
      setTimeout(() => sendToAPI(memory, retryCount + 1), RETRY_DELAYS[retryCount]);
      return;
    }

    if (!response.ok) throw new Error(`HTTP ${response.status}`);

    console.log('[HybridMem] Saved:', memory.text.substring(0, 50));
  } catch (error) {
    if (retryCount < RETRY_DELAYS.length) {
      setTimeout(() => sendToAPI(memory, retryCount + 1), RETRY_DELAYS[retryCount]);
    } else {
      await cacheOffline(memory);
    }
  }
}

// 离线缓存
async function cacheOffline(memory) {
  const cached = await chrome.storage.local.get('offline_queue') || { offline_queue: [] };
  cached.offline_queue.push({ ...memory, retryCount: 0 });
  await chrome.storage.local.set(cached);
}

// 生成消息 ID
function generateMessageId(data) {
  return `${data.platform}-${data.timestamp}-${data.role}`;
}

// 健康检查
chrome.alarms.create('health_check', { periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === 'health_check') {
    try {
      const response = await fetch(`${API_BASE}/api/health`);
      if (response.ok) {
        await retryOfflineQueue();
      }
    } catch (error) {
      console.log('[HybridMem] Service unavailable');
    }
  }
});

// 重试离线队列
async function retryOfflineQueue() {
  const cached = await chrome.storage.local.get('offline_queue');
  if (!cached.offline_queue?.length) return;

  const queue = cached.offline_queue;
  const remaining = [];

  for (const memory of queue) {
    try {
      const response = await fetch(`${API_BASE}/api/memories`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(memory)
      });

      if (!response.ok && memory.retryCount < 3) {
        remaining.push({ ...memory, retryCount: memory.retryCount + 1 });
      }
    } catch {
      if (memory.retryCount < 3) {
        remaining.push({ ...memory, retryCount: memory.retryCount + 1 });
      }
    }
  }

  await chrome.storage.local.set({ offline_queue: remaining });
}

console.log('[HybridMem] Background service worker started');
