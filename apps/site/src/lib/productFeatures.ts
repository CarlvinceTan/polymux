import featureData from '../content/product/features.json';

export type ProductFeatureGroup = 'app' | 'agent';
export type ProductFeatureIcon = 'workspace' | 'hub' | 'drive' | 'browser' | 'memory' | 'tasks' | 'models' | 'skills';
export type ProductFeature = {
  group: ProductFeatureGroup;
  slug: string;
  name: string;
  icon: ProductFeatureIcon;
  menuDescription: string;
  title: string;
  description: string;
  intro: string;
  demoTitle?: string;
  demoItems?: Array<{label: string; title: string; description: string}>;
  benefits: Array<{title: string; description: string}>;
  steps: string[];
  example: {title: string; description: string};
  docsPath: string;
};

export const productFeatures: ProductFeature[] = featureData as ProductFeature[];

export const appFeatures = productFeatures.filter((feature) => feature.group === 'app');
export const agentFeatures = productFeatures.filter((feature) => feature.group === 'agent');

export function productFeaturePath(slug: string): string {
  return `/product/${encodeURIComponent(slug)}/`;
}

export function getProductFeature(slug: string): ProductFeature | undefined {
  return productFeatures.find((feature) => feature.slug === slug);
}
