// Gemini 适配器 (gemini.google.com)
class GeminiAdapter extends BaseAdapter {
  constructor() {
    super('gemini');
  }

  getSelectors() {
    // Gemini uses message-content and model-response containers
    return {
      user: '.query-content, message-content.user-query',
      assistant: '.model-response-text, message-content.model-response'
    };
  }
}

const adapter = new GeminiAdapter();
adapter.start();
