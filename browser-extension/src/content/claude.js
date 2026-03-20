// Claude 适配器 (claude.ai)
class ClaudeAdapter extends BaseAdapter {
  constructor() {
    super('claude');
  }

  getSelectors() {
    // Claude uses data-is-streaming and role-based containers
    // Multiple fallback selectors for different UI versions
    return {
      user: '[data-testid="user-message"], .font-user-message, [data-is-user-message="true"]',
      assistant: '[data-testid="assistant-message"], .font-claude-message, [data-is-user-message="false"] .prose'
    };
  }
}

const adapter = new ClaudeAdapter();
adapter.start();
