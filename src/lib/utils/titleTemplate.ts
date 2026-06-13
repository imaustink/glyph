import { format } from 'date-fns';

export interface TitleToken {
  token: string;
  label: string;
  description: string;
}

export const TITLE_TOKENS: TitleToken[] = [
  { token: '{{date}}',      label: '{{date}}',      description: 'ISO date — 2026-04-18' },
  { token: '{{date-long}}', label: '{{date-long}}', description: 'Long date — April 18, 2026' },
  { token: '{{date-short}}',label: '{{date-short}}',description: 'Short date — Apr 18, 2026' },
  { token: '{{day}}',       label: '{{day}}',       description: 'Day name — Friday' },
  { token: '{{week}}',      label: '{{week}}',      description: 'Week number — W16' },
  { token: '{{month}}',     label: '{{month}}',     description: 'Month + year — April 2026' },
  { token: '{{year}}',      label: '{{year}}',      description: 'Year — 2026' },
  { token: '{{time}}',      label: '{{time}}',      description: 'Time — 14:30' },
];

const RESOLVERS: Record<string, (d: Date) => string> = {
  'date':       (d) => format(d, 'yyyy-MM-dd'),
  'date-long':  (d) => format(d, 'MMMM d, yyyy'),
  'date-short': (d) => format(d, 'MMM d, yyyy'),
  'day':        (d) => format(d, 'EEEE'),
  'week':       (d) => `W${format(d, 'ww')}`,
  'month':      (d) => format(d, 'MMMM yyyy'),
  'year':       (d) => format(d, 'yyyy'),
  'time':       (d) => format(d, 'HH:mm'),
};

export function evaluateTitleTemplate(expr: string): string {
  if (!expr) return '';
  const now = new Date();
  return expr.replace(/\{\{([^}]+)\}\}/g, (_, key: string) => {
    const resolver = RESOLVERS[key.trim()];
    return resolver ? resolver(now) : `{{${key}}}`;
  });
}

type ProseMirrorNode = {
  type: string;
  text?: string;
  content?: ProseMirrorNode[];
  [key: string]: unknown;
};

function evaluateNode(node: ProseMirrorNode, now: Date): ProseMirrorNode {
  if (node.text !== undefined) {
    return {
      ...node,
      text: node.text.replace(/\{\{([^}]+)\}\}/g, (_, key: string) => {
        const resolver = RESOLVERS[key.trim()];
        return resolver ? resolver(now) : `{{${key}}}`;
      })
    };
  }
  if (node.content) {
    return { ...node, content: node.content.map((child) => evaluateNode(child, now)) };
  }
  return node;
}

export function evaluateContentTemplate(contentJson: string): string {
  if (!contentJson) return contentJson;
  try {
    const doc = JSON.parse(contentJson) as ProseMirrorNode;
    const now = new Date();
    return JSON.stringify(evaluateNode(doc, now));
  } catch {
    return contentJson;
  }
}
