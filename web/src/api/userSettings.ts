
export interface UserDefaultModel {
  defaultProvider: string;
  defaultModel: string;
}

export async function fetchUserDefaultModel(): Promise<UserDefaultModel> {
  const response = await fetch('/api/user-settings/default-model');
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({ message: 'Failed to fetch user default model settings' }));
    throw new Error(errorData.message || 'Failed to fetch user default model settings');
  }
  return response.json();
}

export async function patchUserDefaultModel(provider: string, model: string): Promise<void> {
  const response = await fetch('/api/user-settings/default-model', {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ defaultProvider: provider, defaultModel: model }),
  });

  if (!response.ok) {
    try {
      const errorData = await response.json();
      throw new Error(errorData.message || 'Failed to update default model');
    } catch (e) {
      throw new Error('An unknown error occurred while updating the default model', { cause: e });
    }
  }
}
