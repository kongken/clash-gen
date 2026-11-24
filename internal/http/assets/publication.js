const copyLink = document.querySelector('#copy-download-link');
const downloadLink = document.querySelector('#download-link');
const copyFeedback = document.querySelector('#copy-feedback');

copyLink?.addEventListener('click', async () => {
  copyFeedback.hidden = false;
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(downloadLink.value);
    } else {
      downloadLink.select();
      if (!document.execCommand('copy')) throw new Error('Clipboard unavailable');
    }
    copyFeedback.textContent = '订阅链接已复制。';
  } catch {
    downloadLink.select();
    copyFeedback.textContent = '请手动复制已选中的链接。';
  }
});
