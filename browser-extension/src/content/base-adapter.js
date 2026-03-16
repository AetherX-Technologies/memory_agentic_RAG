// 基础适配器类
class BaseAdapter {
  constructor(platform) {
    this.platform = platform;
    this.observer = null;
  }

  // 子类必须实现
  getSelectors() {
    throw new Error('getSelectors must be implemented');
  }

  // 提取消息
  extractMessage(element, role) {
    const text = element.textContent.trim();
    if (!text || text.length < 10) return null;

    return {
      text,
      role,
      platform: this.platform,
      url: window.location.href,
      timestamp: Date.now()
    };
  }

  // 发送到 background
  sendToBackground(message) {
    chrome.runtime.sendMessage({
      type: 'CAPTURE_MESSAGE',
      data: message
    });
  }

  // 启动监听
  start() {
    const selectors = this.getSelectors();

    this.observer = new MutationObserver((mutations) => {
      mutations.forEach((mutation) => {
        mutation.addedNodes.forEach((node) => {
          if (node.nodeType !== 1) return;

          // 检查用户消息
          if (node.matches?.(selectors.user)) {
            const msg = this.extractMessage(node, 'user');
            if (msg) this.sendToBackground(msg);
          }

          // 检查助手消息
          if (node.matches?.(selectors.assistant)) {
            const msg = this.extractMessage(node, 'assistant');
            if (msg) this.sendToBackground(msg);
          }
        });
      });
    });

    this.observer.observe(document.body, {
      childList: true,
      subtree: true
    });
  }

  stop() {
    if (this.observer) {
      this.observer.disconnect();
      this.observer = null;
    }
  }
}
