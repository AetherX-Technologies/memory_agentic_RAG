// Claude 适配器
class ClaudeAdapter extends BaseAdapter {
  constructor() {
    super('claude');
  }

  getSelectors() {
    return {
      user: '[data-test-render-count] .font-user-message',
      assistant: '[data-test-render-count] .font-claude-message'
    };
  }
}

const adapter = new ClaudeAdapter();
adapter.start();

console.log('[HybridMem] Claude adapter started');
