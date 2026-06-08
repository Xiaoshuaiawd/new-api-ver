import React, { useCallback, useMemo, useRef, useState } from 'react';
import {
  Button,
  Input,
  InputNumber,
  Popconfirm,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import CardTable from '../../../../components/common/ui/CardTable';

const { Text } = Typography;

let idCounter = 0;
const uid = () => `srt_${++idCounter}`;

function parseJSON(str, fallback) {
  if (!str || !str.trim()) return fallback;
  try {
    return JSON.parse(str);
  } catch {
    return fallback;
  }
}

function buildRows(value) {
  const map = parseJSON(value, {});
  return Object.entries(map).map(([name, ratio]) => ({
    _id: uid(),
    name,
    ratio: Number.isFinite(Number(ratio)) ? Number(ratio) : 1,
  }));
}

function serializeRows(rows) {
  const map = {};
  rows.forEach((row) => {
    const name = String(row.name || '').trim();
    if (!name) return;
    map[name] = Number.isFinite(Number(row.ratio)) ? Number(row.ratio) : 1;
  });
  return JSON.stringify(map, null, 2);
}

export default function SimpleRatioTable({ value, onChange }) {
  const { t } = useTranslation();
  const [rows, setRows] = useState(() => buildRows(value));
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const emitAndSet = useCallback((updater) => {
    setRows((prev) => {
      const next = typeof updater === 'function' ? updater(prev) : updater;
      onChangeRef.current?.(serializeRows(next));
      return next;
    });
  }, []);

  const updateRow = useCallback(
    (id, field, nextValue) => {
      emitAndSet((prev) =>
        prev.map((row) =>
          row._id === id ? { ...row, [field]: nextValue } : row,
        ),
      );
    },
    [emitAndSet],
  );

  const addRow = useCallback(() => {
    emitAndSet((prev) => {
      const names = new Set(prev.map((row) => row.name));
      let counter = 1;
      let name = `group_${counter}`;
      while (names.has(name)) {
        counter++;
        name = `group_${counter}`;
      }
      return [...prev, { _id: uid(), name, ratio: 1 }];
    });
  }, [emitAndSet]);

  const removeRow = useCallback(
    (id) => {
      emitAndSet((prev) => prev.filter((row) => row._id !== id));
    },
    [emitAndSet],
  );

  const duplicateNames = useMemo(() => {
    const counts = {};
    rows.forEach((row) => {
      const name = String(row.name || '').trim();
      if (!name) return;
      counts[name] = (counts[name] || 0) + 1;
    });
    return new Set(Object.keys(counts).filter((name) => counts[name] > 1));
  }, [rows]);

  const duplicateNamesRef = useRef(duplicateNames);
  duplicateNamesRef.current = duplicateNames;

  const columns = useMemo(
    () => [
      {
        title: t('分组名称'),
        dataIndex: 'name',
        key: 'name',
        width: 220,
        render: (_, record) => (
          <Input
            size='small'
            value={record.name}
            status={
              duplicateNamesRef.current.has(String(record.name).trim())
                ? 'warning'
                : undefined
            }
            onChange={(nextValue) => updateRow(record._id, 'name', nextValue)}
          />
        ),
      },
      {
        title: t('倍率'),
        dataIndex: 'ratio',
        key: 'ratio',
        width: 140,
        render: (_, record) => (
          <InputNumber
            size='small'
            min={0}
            step={0.1}
            value={record.ratio}
            style={{ width: '100%' }}
            onChange={(nextValue) =>
              updateRow(record._id, 'ratio', nextValue ?? 0)
            }
          />
        ),
      },
      {
        title: '',
        key: 'actions',
        width: 50,
        render: (_, record) => (
          <Popconfirm
            title={t('确认删除该分组？')}
            onConfirm={() => removeRow(record._id)}
            position='left'
          >
            <Button
              icon={<IconDelete />}
              type='danger'
              theme='borderless'
              size='small'
            />
          </Popconfirm>
        ),
      },
    ],
    [removeRow, t, updateRow],
  );

  return (
    <div>
      <CardTable
        columns={columns}
        dataSource={rows}
        rowKey='_id'
        hidePagination
        size='small'
        empty={<Text type='tertiary'>{t('暂无分组，点击下方按钮添加')}</Text>}
      />
      <div className='mt-3 flex justify-center'>
        <Button icon={<IconPlus />} theme='outline' onClick={addRow}>
          {t('添加分组')}
        </Button>
      </div>
      {duplicateNames.size > 0 && (
        <Text type='warning' size='small' className='mt-2 block'>
          {t('存在重复的分组名称：')}
          {Array.from(duplicateNames).join(', ')}
        </Text>
      )}
    </div>
  );
}
