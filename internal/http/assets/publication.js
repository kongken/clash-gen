async function copyText(text, field) {
  if (navigator.clipboard) {
    await navigator.clipboard.writeText(text);
  } else {
    field.select();
    if (!document.execCommand('copy')) throw new Error('Clipboard unavailable');
  }
}

const copyLink = document.querySelector('#copy-download-link');
const downloadLink = document.querySelector('#download-link');
const copyFeedback = document.querySelector('#copy-feedback');

copyLink?.addEventListener('click', async () => {
  copyFeedback.hidden = false;
  try {
    await copyText(downloadLink.value, downloadLink);
    copyFeedback.textContent = '订阅链接已复制。';
  } catch {
    downloadLink.select();
    copyFeedback.textContent = '请手动复制已选中的链接。';
  }
});

const copyYaml = document.querySelector('#copy-yaml');
const yamlPreview = document.querySelector('#yaml-preview');
const yamlFeedback = document.querySelector('#copy-yaml-feedback');
const yamlHeading = document.querySelector('#yaml-heading');

if (yamlHeading) {
  yamlHeading.focus({ preventScroll: true });
  yamlHeading.scrollIntoView({ block: 'start' });
}

copyYaml?.addEventListener('click', async () => {
  yamlFeedback.hidden = false;
  try {
    if (!navigator.clipboard) throw new Error('Clipboard unavailable');
    await navigator.clipboard.writeText(yamlPreview.textContent);
    yamlFeedback.textContent = 'YAML 已复制。';
  } catch {
    const selection = getSelection();
    selection.selectAllChildren(yamlPreview);
    yamlFeedback.textContent = '请手动复制已选中的 YAML。';
  }
});
