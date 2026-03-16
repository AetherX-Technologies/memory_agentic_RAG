// Gemini 适配器
class GeminiAdapter extends BaseAdapter {
  constructor() {
    super('gemini');
  }

  getSelectors() {
    return {
      user: '.query-content',
      assistant: '.model-response-text'
    };
  }
}

const adapter = new GeminiAdapter();
adapter.start();

console.log('[HybridMem] Gemini adapter started');
