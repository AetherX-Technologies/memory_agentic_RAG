// ChatGPT 适配器 (chatgpt.com)
class ChatGPTAdapter extends BaseAdapter {
  constructor() {
    super('chatgpt');
  }

  getSelectors() {
    // data-message-author-role is the primary stable attribute
    return {
      user: '[data-message-author-role="user"]',
      assistant: '[data-message-author-role="assistant"]'
    };
  }
}

const adapter = new ChatGPTAdapter();
adapter.start();
