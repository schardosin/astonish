
import React from 'react';

interface ModelCredentialBannerProps {
  pinnedProvider: string;
  pinnedModel: string;
  effectiveProvider: string;
  effectiveModel: string;
  onPickAnother: () => void;
}

const ModelCredentialBanner: React.FC<ModelCredentialBannerProps> = ({
  pinnedProvider,
  pinnedModel,
  effectiveProvider,
  effectiveModel,
  onPickAnother,
}) => {
  if (!pinnedModel) {
    return null;
  }

  const showBanner = pinnedModel !== effectiveModel;

  if (!showBanner) {
    return null;
  }

  return (
    <div className="bg-amber-500/10 border border-amber-500/30 text-amber-700 dark:text-amber-400 rounded px-3 py-2 text-sm">
      Model '{pinnedModel}' unavailable — using '{effectiveModel}' instead.{' '}
      <button onClick={onPickAnother} className="underline font-medium">
        Pick another model.
      </button>
    </div>
  );
};

export default ModelCredentialBanner;
