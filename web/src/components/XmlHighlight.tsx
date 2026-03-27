import { useMemo } from 'react';

interface Props {
  code: string;
  className?: string;
}

// Lightweight XML syntax highlighter — no dependencies.
// Splits XML into tokens and wraps each in a styled span.
export function XmlHighlight({ code, className }: Props) {
  const parts = useMemo(() => tokenize(code), [code]);

  return (
    <code className={className}>
      {parts.map((part, i) => (
        <span key={i} className={styles[part.type]}>
          {part.text}
        </span>
      ))}
    </code>
  );
}

type TokenType = 'tag' | 'attr' | 'string' | 'text' | 'comment';

const styles: Record<TokenType, string> = {
  tag: 'text-blue-400',
  attr: 'text-yellow-300',
  string: 'text-green-400',
  text: 'text-foreground',
  comment: 'text-muted-foreground italic',
};

interface Token {
  type: TokenType;
  text: string;
}

function tokenize(xml: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;

  while (i < xml.length) {
    // XML comment
    if (xml.startsWith('<!--', i)) {
      const end = xml.indexOf('-->', i);
      const close = end === -1 ? xml.length : end + 3;
      tokens.push({ type: 'comment', text: xml.slice(i, close) });
      i = close;
      continue;
    }

    // XML tag
    if (xml[i] === '<') {
      const end = xml.indexOf('>', i);
      if (end === -1) {
        tokens.push({ type: 'text', text: xml.slice(i) });
        break;
      }
      const tag = xml.slice(i, end + 1);
      tokenizeTag(tag, tokens);
      i = end + 1;
      continue;
    }

    // Text content
    const next = xml.indexOf('<', i);
    const text = next === -1 ? xml.slice(i) : xml.slice(i, next);
    if (text) tokens.push({ type: 'text', text });
    i = next === -1 ? xml.length : next;
  }

  return tokens;
}

function tokenizeTag(tag: string, tokens: Token[]) {
  // Match: < or </ then tag name
  const nameMatch = tag.match(/^(<\/?)([\w:.-]+)/);
  if (!nameMatch) {
    tokens.push({ type: 'tag', text: tag });
    return;
  }

  tokens.push({ type: 'tag', text: nameMatch[1] });
  tokens.push({ type: 'tag', text: nameMatch[2] });

  const rest = tag.slice(nameMatch[0].length);

  // Match attributes: name="value" or name='value'
  const attrRe = /([\w:.-]+)(=)("[^"]*"|'[^']*')/g;
  let last = 0;
  let m;
  while ((m = attrRe.exec(rest)) !== null) {
    if (m.index > last) {
      tokens.push({ type: 'tag', text: rest.slice(last, m.index) });
    }
    tokens.push({ type: 'attr', text: m[1] });
    tokens.push({ type: 'tag', text: m[2] });
    tokens.push({ type: 'string', text: m[3] });
    last = m.index + m[0].length;
  }

  if (last < rest.length) {
    tokens.push({ type: 'tag', text: rest.slice(last) });
  }
}
