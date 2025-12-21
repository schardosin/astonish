import { Edit3 } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function InputNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={Edit3} 
      nodeType="Input"
      iconColor="#a78bfa"
    />
  )
}
