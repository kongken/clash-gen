for (const section of document.querySelectorAll('[data-draft-rows]')) {
  const list = section.querySelector('[data-row-list]');
  const template = section.querySelector('[data-row-template]');

  function updateRows() {
    const rows = Array.from(list.children);
    rows.forEach((row, index) => {
      row.querySelector('[data-row-number]').textContent = index + 1;
      row.querySelector('[data-row-action="up"]').disabled = index === 0;
      row.querySelector('[data-row-action="down"]').disabled = index === rows.length - 1;
    });
  }

  section.addEventListener('click', (event) => {
    const button = event.target.closest('button[data-row-action]');
    if (!button) return;
    const row = button.closest('[data-row]');

    switch (button.dataset.rowAction) {
      case 'add': {
        const newRow = template.content.cloneNode(true);
        const firstField = newRow.querySelector('input:not([type=hidden]), select');
        list.append(newRow);
        firstField?.focus();
        break;
      }
      case 'remove':
        row.remove();
        section.querySelector('[data-row-action="add"]').focus();
        break;
      case 'up':
        row.previousElementSibling?.before(row);
        break;
      case 'down':
        row.nextElementSibling?.after(row);
        break;
    }
    updateRows();
  });

  updateRows();
}
