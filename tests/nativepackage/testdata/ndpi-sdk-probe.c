// SPDX-License-Identifier: BSD-3-Clause
// SDK-only diagnostic. nDPI 6 uses its LGPL-only mode; no product entitlement is assumed.
#include <stdio.h>
#include <stdlib.h>
#include <ndpi_api.h>
int main(int argc,char **argv) {
 if(argc!=2) return 2;
 unsigned char packet[4096];FILE *f=fopen(argv[1],"rb");if(!f)return 3;
 size_t size=fread(packet,1,sizeof(packet),f);fclose(f);
 struct ndpi_detection_module_struct *mod;
#if defined(NDPI_MAJOR) && NDPI_MAJOR >= 6
 mod=ndpi_init_detection_module(NULL,NDPI_LICENSE_FOR_PROFIT_LGPL);
#elif defined(NDPI_MAJOR) && NDPI_MAJOR >= 5
 mod=ndpi_init_detection_module(NULL);
#else
 NDPI_PROTOCOL_BITMASK all;
 mod=ndpi_init_detection_module(ndpi_no_prefs);
 if(mod) { NDPI_BITMASK_SET_ALL(all); ndpi_set_protocol_detection_bitmask2(mod,&all); }
#endif
 if(!mod) return 4;
 ndpi_finalize_initialization(mod);
 struct ndpi_flow_struct *flow=calloc(1,sizeof(*flow));if(!flow)return 5;
 #if defined(NDPI_MAJOR) && NDPI_MAJOR >= 5
 ndpi_protocol p=ndpi_detection_process_packet(mod,flow,packet,size,100000,NULL);
 unsigned int master=p.proto.master_protocol,app=p.proto.app_protocol;
#else
 ndpi_protocol p=ndpi_detection_process_packet(mod,flow,packet,size,100000);
 unsigned int master=p.master_protocol,app=p.app_protocol;
#endif
 printf("version=%s master=%s app=%s sni=%s\n",ndpi_revision(),ndpi_get_proto_name(mod,master),ndpi_get_proto_name(mod,app),flow->host_server_name);
 ndpi_free_flow(flow);ndpi_exit_detection_module(mod);return 0;
}
