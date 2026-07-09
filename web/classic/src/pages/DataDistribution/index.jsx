/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  Form,
  Row,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import {
  API,
  getTodayStartTimestamp,
  renderNumber,
  showError,
  timestamp2string,
} from '../../helpers';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';

const emptyData = {
  overview: {
    total_count: 0,
    success_count: 0,
    fail_count: 0,
    tools_count: 0,
    tools_percent: 0,
    prompt_tokens_sum: 0,
    completion_tokens_sum: 0,
    total_tokens_sum: 0,
  },
  round_distribution: [],
  prompt_tokens_distribution: [],
  total_tokens_distribution: [],
  model_distribution: [],
  daily_stats: [],
  db_compare: {
    chat_log_count: 0,
    logs_count: 0,
    chat_log_prompt_tokens: 0,
    logs_prompt_tokens: 0,
    chat_log_total_tokens: 0,
    logs_total_tokens: 0,
    count_diff: 0,
    prompt_tokens_diff: 0,
    total_tokens_diff: 0,
    count_diff_percent: 0,
  },
  scan_tables: [],
};

function buildPieSpec(data, title) {
  return {
    type: 'pie',
    title: {
      visible: true,
      text: title,
    },
    data: [
      {
        id: 'pie',
        values: data.map((item) => ({
          type: item.label || item.model_name,
          value: item.count || 0,
        })),
      },
    ],
    categoryField: 'type',
    valueField: 'value',
    legends: {
      visible: true,
      orient: 'bottom',
    },
    tooltip: {
      visible: true,
    },
    label: {
      visible: true,
    },
  };
}

function DataDistributionPage() {
  const { t } = useTranslation();
  const [formApi, setFormApi] = useState(null);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(emptyData);

  const now = new Date();
  const formInitValues = {
    dateRange: [
      timestamp2string(getTodayStartTimestamp()),
      timestamp2string(Math.floor(now.getTime() / 1000)),
    ],
    status: 'all',
  };

  const requestData = async () => {
    const formValues = formApi ? formApi.getValues() : formInitValues;
    const dateRange = formValues.dateRange || formInitValues.dateRange;
    const startTimestamp = Math.floor(Date.parse(dateRange[0]) / 1000);
    const endTimestamp = Math.floor(Date.parse(dateRange[1]) / 1000);
    const status = formValues.status || 'all';

    setLoading(true);
    try {
      const res = await API.get(
        `/api/admin/data-distribution?start_timestamp=${startTimestamp}&end_timestamp=${endTimestamp}&status=${status}`,
      );
      const { success, message, data: apiData } = res.data;
      if (!success) {
        showError(message || t('查询失败'));
        return;
      }
      setData(apiData || emptyData);
    } catch (error) {
      showError(t('查询失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (formApi) {
      requestData();
    }
  }, [formApi]);

  const overviewCards = useMemo(
    () => [
      { title: t('总存储条数'), value: renderNumber(data.overview.total_count) },
      { title: t('成功条数'), value: renderNumber(data.overview.success_count) },
      { title: t('失败条数'), value: renderNumber(data.overview.fail_count) },
      {
        title: t('Tools条数'),
        value: `${renderNumber(data.overview.tools_count)} (${(data.overview.tools_percent || 0).toFixed(2)}%)`,
      },
      {
        title: t('Prompt Tokens总和'),
        value: renderNumber(data.overview.prompt_tokens_sum),
      },
      {
        title: t('Total Tokens总和'),
        value: renderNumber(data.overview.total_tokens_sum),
      },
    ],
    [data.overview, t],
  );

  const roundPieSpec = useMemo(
    () => buildPieSpec(data.round_distribution || [], t('轮次分布')),
    [data.round_distribution, t],
  );
  const promptPieSpec = useMemo(
    () =>
      buildPieSpec(data.prompt_tokens_distribution || [], t('Prompt分布')),
    [data.prompt_tokens_distribution, t],
  );
  const totalPieSpec = useMemo(
    () => buildPieSpec(data.total_tokens_distribution || [], t('Total分布')),
    [data.total_tokens_distribution, t],
  );
  const modelPieSpec = useMemo(
    () => buildPieSpec(data.model_distribution || [], t('模型调用分布')),
    [data.model_distribution, t],
  );

  const modelColumns = [
    { title: t('模型'), dataIndex: 'model_name', key: 'model_name' },
    { title: t('次数'), dataIndex: 'count', key: 'count' },
    {
      title: t('占比'),
      dataIndex: 'percent',
      key: 'percent',
      render: (v) => `${(v || 0).toFixed(2)}%`,
    },
  ];

  const dailyColumns = [
    { title: t('日期'), dataIndex: 'date', key: 'date' },
    { title: t('总条数'), dataIndex: 'total_count', key: 'total_count' },
    { title: t('成功'), dataIndex: 'success_count', key: 'success_count' },
    { title: t('失败'), dataIndex: 'fail_count', key: 'fail_count' },
    { title: t('Tools'), dataIndex: 'tools_count', key: 'tools_count' },
    {
      title: t('Prompt总和'),
      dataIndex: 'prompt_tokens_sum',
      key: 'prompt_tokens_sum',
    },
    {
      title: t('Completion总和'),
      dataIndex: 'completion_tokens_sum',
      key: 'completion_tokens_sum',
    },
    {
      title: t('Total总和'),
      dataIndex: 'total_tokens_sum',
      key: 'total_tokens_sum',
    },
    {
      title: t('平均延迟(ms)'),
      dataIndex: 'avg_latency_ms',
      key: 'avg_latency_ms',
      render: (v) => (v || 0).toFixed(2),
    },
    {
      title: t('TOP1模型'),
      dataIndex: 'top_model_name',
      key: 'top_model_name',
      render: (v, row) =>
        v ? `${v} (${renderNumber(row.top_model_count || 0)})` : '-',
    },
  ];

  return (
    <div className='mt-[60px] px-2'>
      <Card className='!rounded-2xl'>
        <Form
          initValues={formInitValues}
          getFormApi={(api) => setFormApi(api)}
          onSubmit={requestData}
          layout='vertical'
        >
          <Row gutter={12}>
            <Col span={12}>
              <Form.DatePicker
                field='dateRange'
                type='dateTimeRange'
                showClear
                pure
                size='small'
                presets={DATE_RANGE_PRESETS.map((preset) => ({
                  text: t(preset.text),
                  start: preset.start(),
                  end: preset.end(),
                }))}
              />
            </Col>
            <Col span={4}>
              <Form.Select field='status' size='small' pure>
                <Form.Select.Option value='all'>{t('全部')}</Form.Select.Option>
                <Form.Select.Option value='success'>
                  {t('成功')}
                </Form.Select.Option>
                <Form.Select.Option value='fail'>{t('失败')}</Form.Select.Option>
              </Form.Select>
            </Col>
            <Col span={8}>
              <div className='flex justify-end gap-2'>
                <Button type='primary' htmlType='submit' loading={loading}>
                  {t('查询')}
                </Button>
                <Button
                  onClick={() => {
                    if (formApi) {
                      formApi.reset();
                    }
                  }}
                >
                  {t('重置')}
                </Button>
              </div>
            </Col>
          </Row>
        </Form>
      </Card>

      <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 mt-3'>
        {overviewCards.map((item) => (
          <Card key={item.title} className='!rounded-2xl'>
            <Typography.Text type='tertiary'>{item.title}</Typography.Text>
            <div className='text-lg font-semibold mt-1'>{item.value}</div>
          </Card>
        ))}
      </div>

      <Card className='!rounded-2xl mt-3' title={t('日志库对比')}>
        <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3'>
          <div>
            <Typography.Text type='tertiary'>{t('调用次数差值')}</Typography.Text>
            <div className='text-lg font-semibold'>
              {renderNumber(data.db_compare.count_diff)} (
              {(data.db_compare.count_diff_percent || 0).toFixed(2)}%)
            </div>
          </div>
          <div>
            <Typography.Text type='tertiary'>
              {t('Prompt差值')}
            </Typography.Text>
            <div className='text-lg font-semibold'>
              {renderNumber(data.db_compare.prompt_tokens_diff)}
            </div>
          </div>
          <div>
            <Typography.Text type='tertiary'>{t('Total差值')}</Typography.Text>
            <div className='text-lg font-semibold'>
              {renderNumber(data.db_compare.total_tokens_diff)}
            </div>
          </div>
        </div>
      </Card>

      <div className='grid grid-cols-1 lg:grid-cols-2 gap-3 mt-3'>
        <Card className='!rounded-2xl'>
          <div className='h-[360px]'>
            <VChart spec={roundPieSpec} />
          </div>
        </Card>
        <Card className='!rounded-2xl'>
          <div className='h-[360px]'>
            <VChart spec={modelPieSpec} />
          </div>
        </Card>
        <Card className='!rounded-2xl'>
          <div className='h-[360px]'>
            <VChart spec={promptPieSpec} />
          </div>
        </Card>
        <Card className='!rounded-2xl'>
          <div className='h-[360px]'>
            <VChart spec={totalPieSpec} />
          </div>
        </Card>
      </div>

      <Card className='!rounded-2xl mt-3' title={t('每日统计')}>
        <Table
          rowKey='date'
          columns={dailyColumns}
          dataSource={data.daily_stats || []}
          pagination={false}
          size='small'
        />
      </Card>

      <Card className='!rounded-2xl mt-3' title={t('模型分布明细')}>
        <Table
          rowKey='model_name'
          columns={modelColumns}
          dataSource={data.model_distribution || []}
          pagination={false}
          size='small'
        />
      </Card>

      <Card className='!rounded-2xl mt-3' title={t('分表扫描状态')}>
        <div className='flex flex-wrap gap-2'>
          {(data.scan_tables || []).map((item) => (
            <Tag
              key={`${item.table}-${item.date}`}
              color={item.status === 'ok' ? 'green' : 'grey'}
            >
              {item.table}
            </Tag>
          ))}
        </div>
      </Card>
    </div>
  );
}

export default DataDistributionPage;
