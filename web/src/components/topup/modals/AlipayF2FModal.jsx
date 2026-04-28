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

import React from 'react';
import { Modal, Typography, Space, Tag } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { SiAlipay } from 'react-icons/si';

const { Text, Paragraph } = Typography;

const statusColorMap = {
  pending: 'blue',
  success: 'green',
  failed: 'red',
  expired: 'amber',
};

export default function AlipayF2FModal({
  t,
  visible,
  onCancel,
  qrCode,
  tradeNo,
  status,
  tradeStatus,
  payMoney,
}) {
  return (
    <Modal
      title={
        <div className='flex items-center gap-2'>
          <SiAlipay size={18} color='#1677FF' />
          {t('支付宝当面付')}
        </div>
      }
      visible={visible}
      onCancel={onCancel}
      footer={null}
      centered
      maskClosable={false}
    >
      <Space vertical align='center' style={{ width: '100%' }}>
        <Text strong>{t('请使用支付宝扫一扫完成付款')}</Text>
        {qrCode ? (
          <div className='rounded-2xl bg-white p-4 shadow-sm'>
            <QRCodeSVG value={qrCode} size={220} />
          </div>
        ) : null}
        <Tag color={statusColorMap[status] || 'grey'}>
          {status === 'success'
            ? t('已支付')
            : status === 'expired'
              ? t('已过期')
              : status === 'failed'
                ? t('支付失败')
                : t('等待支付')}
        </Tag>
        {payMoney ? (
          <Text type='secondary'>
            {t('实付金额')}：{payMoney} {t('元')}
          </Text>
        ) : null}
        {tradeNo ? (
          <Paragraph
            copyable={{ content: tradeNo }}
            style={{ marginBottom: 0, textAlign: 'center' }}
          >
            {t('订单号')}：{tradeNo}
          </Paragraph>
        ) : null}
        {tradeStatus ? (
          <Text type='tertiary'>
            {t('支付状态')}：{tradeStatus}
          </Text>
        ) : null}
        <Text type='secondary'>{t('支付完成后会自动刷新订单状态并入账')}</Text>
      </Space>
    </Modal>
  );
}
