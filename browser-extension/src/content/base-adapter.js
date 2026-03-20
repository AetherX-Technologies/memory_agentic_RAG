// 基础适配器类 — 所有平台适配器的父类
class BaseAdapter {
  constructor(platform) {
    this.platform = platform;
    this.observer = null;
    this.processedHashes = new Set(); // 去重
    this.debounceTimer = null;
    this.pendingNodes = [];
  }

  // 子类必须实现：返回 { user: 'selector', assistant: 'selector' }
  getSelectors() {
    throw new Error('getSelectors must be implemented');
  }

  // 生成消息内容哈希（去重用）
  hashMessage(text, role) {
    const content = `${role}:${text.substring(0, 100)}:${window.location.href}`;
    let hash = 0;
    for (let i = 0; i < content.length; i++) {
      hash = ((hash << 5) - hash + content.charCodeAt(i)) | 0;
    }
    return hash.toString(36);
  }

  // 提取消息
  extractMessage(element, role) {
    try {
      const text = element.textContent?.trim();
      if (!text || text.length < 10) return null;

      // 去重
      const hash = this.hashMessage(text, role);
      if (this.processedHashes.has(hash)) return null;
      this.processedHashes.add(hash);

      // 防止去重集合无限增长
      if (this.processedHashes.size > 1000) {
        const iter = this.processedHashes.values();
        for (let i = 0; i < 200; i++) {
          this.processedHashes.delete(iter.next().value);
        }
      }

      return {
        text,
        role,
        platform: this.platform,
        url: window.location.href,
        timestamp: Date.now()
      };
    } catch (error) {
      console.error(`[HybridMem] Error extracting message:`, error);
      return null;
    }
  }

  // 发送到 background
  sendToBackground(message) {
    try {
      chrome.runtime.sendMessage({
        type: 'CAPTURE_MESSAGE',
        data: message
      });
    } catch (error) {
      console.error(`[HybridMem] Error sending to background:`, error);
    }
  }

  // 处理单个节点（检查自身和子元素）
  processNode(node) {
    if (node.nodeType !== 1) return; // Element nodes only

    const selectors = this.getSelectors();

    try {
      // 检查节点自身
      if (node.matches?.(selectors.user)) {
        const msg = this.extractMessage(node, 'user');
        if (msg) this.sendToBackground(msg);
      } else if (node.matches?.(selectors.assistant)) {
        const msg = this.extractMessage(node, 'assistant');
        if (msg) this.sendToBackground(msg);
      }

      // 检查子元素（处理嵌套 DOM 结构）
      for (const child of node.querySelectorAll?.(selectors.user) || []) {
        const msg = this.extractMessage(child, 'user');
        if (msg) this.sendToBackground(msg);
      }
      for (const child of node.querySelectorAll?.(selectors.assistant) || []) {
        const msg = this.extractMessage(child, 'assistant');
        if (msg) this.sendToBackground(msg);
      }
    } catch (error) {
      console.error(`[HybridMem] Error processing node:`, error);
    }
  }

  // 防抖处理：收集快速连续的 DOM 变更，2秒后统一处理
  debouncedProcess(nodes) {
    this.pendingNodes.push(...nodes);

    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
    }

    this.debounceTimer = setTimeout(() => {
      const toProcess = this.pendingNodes.splice(0);
      for (const node of toProcess) {
        this.processNode(node);
      }
    }, 2000); // 等待流式响应完成
  }

  // 启动监听
  start() {
    // 先捕获页面上已有的消息
    this.captureExisting();

    this.observer = new MutationObserver((mutations) => {
      const addedNodes = [];
      for (const mutation of mutations) {
        for (const node of mutation.addedNodes) {
          addedNodes.push(node);
        }
      }
      if (addedNodes.length > 0) {
        this.debouncedProcess(addedNodes);
      }
    });

    this.observer.observe(document.body, {
      childList: true,
      subtree: true
    });

    console.log(`[HybridMem] ${this.platform} adapter started`);
  }

  // 捕获页面上已存在的消息（页面刷新后）
  captureExisting() {
    try {
      const selectors = this.getSelectors();
      for (const el of document.querySelectorAll(selectors.user)) {
        const msg = this.extractMessage(el, 'user');
        if (msg) this.sendToBackground(msg);
      }
      for (const el of document.querySelectorAll(selectors.assistant)) {
        const msg = this.extractMessage(el, 'assistant');
        if (msg) this.sendToBackground(msg);
      }
    } catch (error) {
      console.error(`[HybridMem] Error capturing existing messages:`, error);
    }
  }

  stop() {
    if (this.observer) {
      this.observer.disconnect();
      this.observer = null;
    }
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    this.pendingNodes = [];
  }
}
