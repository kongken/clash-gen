function updateNodeFields(row) {
  const protocolField = row.querySelector('[name="node_type"]');
  if (!protocolField) return;

  const protocol = protocolField.value;
  const network = row.querySelector('[name="node_network"]').value;
  const security = row.querySelector('[name="node_security"]');
  const allowsReality = protocol === 'vless' && network === 'tcp';
  const realityOption = security.querySelector('[value="reality"]');
  realityOption.disabled = !allowsReality;
  realityOption.hidden = !allowsReality;
  if (!allowsReality && security.value === 'reality') security.value = 'tls';

  const features = {
    tls: protocol === 'trojan' || (protocol !== 'ss' && security.value !== ''),
    ws: protocol !== 'ss' && network === 'ws',
    reality: allowsReality && security.value === 'reality',
  };
  for (const field of row.querySelectorAll('[data-node-protocols]')) {
    field.hidden = !field.dataset.nodeProtocols.split(' ').includes(protocol);
  }
  for (const field of row.querySelectorAll('[data-node-feature]')) {
    field.hidden = !features[field.dataset.nodeFeature];
  }
}

for (const section of document.querySelectorAll('[data-draft-rows]')) {
  const list = section.querySelector('[data-row-list]');
  const template = section.querySelector('[data-row-template]');

  function updateRows() {
    const rows = Array.from(list.children);
    const count = section.querySelector('[data-row-count]');
    if (count) count.textContent = rows.length;
    rows.forEach((row, index) => {
      const number = row.querySelector('[data-row-number]');
      if (number) number.textContent = index + 1;
      updateNodeFields(row);
      row.querySelector('[data-row-action="up"]').disabled = index === 0;
      row.querySelector('[data-row-action="down"]').disabled = index === rows.length - 1;
    });
  }

  section.addEventListener('change', (event) => {
    if (event.target.matches('[name="node_type"], [name="node_network"], [name="node_security"]')) {
      updateNodeFields(event.target.closest('[data-row]'));
    }
  });

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
