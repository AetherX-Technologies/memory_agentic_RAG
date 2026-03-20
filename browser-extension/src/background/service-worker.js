// API 配置
const API_BASE = 'http://localhost:8080';
const RETRY_DELAYS = [1000, 2000, 5000];
const MAX_OFFLINE_QUEUE = 500;

// 已处理消息 ID 去重集合
const processedMessages = new Set();

// 消息处理
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'CAPTURE_MESSAGE') {
    handleMessage(message.data);
  }
  if (message.type === 'RETRY_QUEUE') {
    retryOfflineQueue().then(() => sendResponse({ ok: true }));
    return true; // async response
  }
});

// 处理捕获的消息
async function handleMessage(data) {
  // 去重：基于消息指纹
  const msgId = generateMessageId(data);
  if (processedMessages.has(msgId)) {
    return;
  }
  processedMessages.add(msgId);

  // 防止去重集合无限增长（保留最近 2000 条）
  if (processedMessages.size > 2000) {
    const iter = processedMessages.values();
    for (let i = 0; i < 500; i++) {
      processedMessages.delete(iter.next().value);
    }
  }

  const memory = {
    text: data.text,
    category: 'conversation',
    scope: `browser:${data.platform}`,
    importance: 0.5,
    timestamp: data.timestamp,
    metadata: JSON.stringify({
      source: 'browser-plugin',
      url: data.url,
      role: data.role,
      platform: data.platform,
      messageId: msgId
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

    if (response.status >= 500) {
      if (retryCount < RETRY_DELAYS.length) {
        setTimeout(() => sendToAPI(memory, retryCount + 1), RETRY_DELAYS[retryCount]);
      } else {
        await cacheOffline(memory); // all retries exhausted
      }
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

// 离线缓存（带容量限制）
async function cacheOffline(memory) {
  const result = await chrome.storage.local.get('offline_queue');
  const queue = result.offline_queue || [];

  // 容量限制：丢弃最旧的消息
  if (queue.length >= MAX_OFFLINE_QUEUE) {
    queue.splice(0, queue.length - MAX_OFFLINE_QUEUE + 1);
  }

  queue.push({ ...memory, retryCount: 0, cachedAt: Date.now() });
  await chrome.storage.local.set({ offline_queue: queue });
  console.log(`[HybridMem] Cached offline (queue: ${queue.length})`);
}

// 生成消息指纹（用于去重）
function generateMessageId(data) {
  // 使用平台+角色+内容前100字符的哈希
  const content = `${data.platform}:${data.role}:${data.text.substring(0, 100)}:${data.url}`;
  let hash = 0;
  for (let i = 0; i < content.length; i++) {
    hash = ((hash << 5) - hash + content.charCodeAt(i)) | 0;
  }
  return `msg-${hash.toString(36)}`;
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
  const result = await chrome.storage.local.get('offline_queue');
  const queue = result.offline_queue || [];
  if (queue.length === 0) return;

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
      // retryCount >= 3: silently discard
    } catch {
      if (memory.retryCount < 3) {
        remaining.push({ ...memory, retryCount: memory.retryCount + 1 });
      }
    }
  }

  await chrome.storage.local.set({ offline_queue: remaining });
  console.log(`[HybridMem] Retried queue: ${queue.length - remaining.length} sent, ${remaining.length} remaining`);
}

console.log('[HybridMem] Background service worker started');
