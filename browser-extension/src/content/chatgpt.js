// ChatGPT 适配器
class ChatGPTAdapter extends BaseAdapter {
  constructor() {
    super('chatgpt');
  }

  getSelectors() {
    return {
      user: '[data-message-author-role="user"]',
      assistant: '[data-message-author-role="assistant"]'
    };
  }
}

// 启动适配器
const adapter = new ChatGPTAdapter();
adapter.start();

console.log('[HybridMem] ChatGPT adapter started');
